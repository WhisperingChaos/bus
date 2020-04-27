/*
Package bus streams arbitrary Message types between Sender(s) and Receiver(s)
using a shared conduit - single reference counted channel.  Once the last
Sender disconnects from the bus, the bus shuts down.  A bus shutdown
closes the channel forcing the Receivers to abandon the bus.
*/
package bus

import (
	"sync"
)

/*
B semantics

- Senders generate Messages that are consumed by Receivers.  There isn't an address
mechanism, therefore, Messages can't be directed to specific Receivers. Any available
Receiver will attempt to consume a Message.  Try to avoid Message loops in situations
involving an atomic unit of concurrency which incorporates both a Sender and Receiver.
This situation can more directly trigger infinite Message looping/deadlock.

- Messages sent across the bus are delivered as the interface{} type.  Encode
a type switch statement (https://golang.org/ref/spec#TypeSwitchStmt) in a Receiver
to open the interface{} envelope and reveal its contents.  The disavantage of
this downcast approach is the lost of static type checking to ensure program
correctness during compilation.

- B is implemented as a single unbuffered channel.  Senders and Receivers can
attach themselves in any order.  However, the channel will block until a companion
instance (Sender needs a Receiver, Receiver needs a Sender) begins processing
Messages.  Although Senders/Receivers can abitrarily connect to a bus, consideration
should be applied to ensure that all intended Senders attach to the bus before it
terminates.  For example, one could employ a mechanism, like a WaitGroup or channel
to enforce a Happens Before (https://golang.org/ref/mem) relationship to guarantee
that all Senders connect before connecting/starting any Receiver.

- B is concurrency safe - a single instance can be shared among goroutines,
therefore, its methods can briefly block.  Since B employs a mutex for concurrency
safety, blocked goroutines adopt mutex queuing semantics.

- B is cooperatively owned.  The bus shuts down after all Senders disconnect from it -
reference counted resource management.  Therefore, a Sender must notify the bus when it
disconnects.  Senders can unilaterally disconnect from a bus.  Receivers can also
disconnect at will but do so without notification.  When a bus shuts down,
all Receivers are "forced" off the bus via a channel closed signal.

- Once a bus shuts down, attempts to attach a new Sender will fail with
notification.  However, Receivers can attach and access the shared channel even after
a bus shutdown.  In this situation, the Receiver, when it attempts to access the
channel, is notified that it's closed.

Motivation

- Provides a minimal interface to expose a rudimentary abilility to share a
single channel with little concern to its management.

- This implementation targets small workloads as it uses a single
physical channel.  It causes all Senders/Receivers to synchronize on this single
limited resource.  To gauge its performance, run the example Benchmark located
in its test file.
*/
type B struct {
	wg   int32
	once sync.Once
	l    sync.Mutex
	term bool
	t    chan struct{}
	c    chan interface{}
}

/*
SenderConnect relates a Sender to a given bus instance.  The Sender acquires a channel
that allows it to disptach arbitrary message types over the bus, as well as a
disconnect function.  A Sender executes the disconnect function to notify
the bus that it no longer needs to dispatch messages through it.  Forgetting to
issue this notification will cause the bus to remain forever allocated.

The provided channel will block until a connected Receiver becomes available
to consume messages from this channel.

Attempting to connect to an already shutdown bus will return 'false' for 'active'.
*/
func (b *B) SenderConnect() (send chan<- interface{}, disconnect func(), active bool) {
	b.l.Lock()
	defer b.l.Unlock()
	const maxInt32 = 2147483647
	b.once.Do(b.init())
	if b.term {
		return nil, nil, false
	}
	if b.wg == maxInt32 {
		panic("too many senders on bus")
	}
	b.wg++
	disconnect = func() {
		b.coopTerm()
	}
	return b.c, disconnect, true
}

/*
ReceiverConnect relates a Receiver to the provided bus instance.  The Receiver
acquires a channel allowing it to consume arbitary Message types over the bus.
A Receiver can unilaterally disconnect itself from its bus.  However, when
a bus shuts down, the aquired channel is closed, forcing a Receiver
to terminate its bus' connection.

If the bus is active, the provided channel will block until a connected
Sender generates a messages.  If more than one Receiver is connected
to a bus, in golang versions prior to 1.14 (introduces preemption),
other Receivers may never process a message.  They may remain blocked
until the bus shuts down.

To avoid Message loops, a Receiver should be capable of processing every
message type produced by every Sender connected to the bus.  Otherwise,
carefully craft the Receiver(s) and Sender(s) to avoid infinite message
loops that may arise from flawed retry schemes.

A Receiver does not offer a disconnect function as returned by SenderConnect.
Unlike a Sender, the design doesn't confer ownership to a Receivers. This
asymetry simplifies both the implementation and interface of this bus
class and delivers semantics similar to a basic channel.
*/
func (b *B) ReceiverConnect() (receive <-chan interface{}) {
	// Mutex not required here as "once" will block others
	// from continuing until after it returns, b.c is
	// is never written to again during the lifetime of bus,
	// no other B data members are accessed within this
	// method, all other methods requiring init also
	// block, and a channel manages concurrent access
	// to its internal resources.
	b.once.Do(b.init())
	return b.c
}

/*
ShutdownMonitor enables Observers, not interested in receiving Messages,
to determine if a bus has terminated.  A terminated bus notifies
Observers by closing the returned shutdown channel.  Otherwise,
while active, this channel will remain blocked.

A closed ShutdownMonitor indicates that all Senders on a bus have
disconnected from it.  However, Receivers may continue to process
messages from the bus.
*/
func (b *B) ShutdownMonitor() (shutdown <-chan struct{}) {
	// Mutex not required here as "once" will block others
	// from continuing until after it returns, b.t is
	// is never written to again during the lifetime of bus,
	// no other B data members are accessed within this
	// method, all other methods requiring init also
	// block, and a channel manages concurrent access
	// to its internal resources.
	b.once.Do(b.init())
	return b.t
}
func (b *B) init() func() {
	return func() {
		b.c = make(chan interface{})
		b.t = make(chan struct{})
	}
}
func (b *B) coopTerm() {
	b.l.Lock()
	defer b.l.Unlock()
	b.wg--
	if b.wg > 0 {
		return
	}
	if b.wg < 0 {
		panic("logic error one too many disconnects")
	}
	close(b.c)
	close(b.t)
	b.term = true
}
