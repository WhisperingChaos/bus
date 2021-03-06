package bus

import (
	"fmt"
	"io"
	"testing"

	"github.com/stretchr/testify/assert"
)

func Test(t *testing.T) {
	assrt := assert.New(t)
	var b B
	assrt.True(sendN(1, &b))
	assrt.True(sendN(2, &b))
	assrt.True(sendN(3, &b))
	assrt.True(sendN(4, &b))
	assrt.True(sendN(5, &b))
	assrt.True(sendN(6, &b))
	assrt.True(sendN(7, &b))
	assrt.Equal(7, receive(b.ReceiverConnect()))
}

func Test_BusAlreadyTerminated(t *testing.T) {
	assrt := assert.New(t)
	var b B

	// bus started
	assrt.True(sendN(1, &b))
	assrt.Equal(1, receive(b.ReceiverConnect()))
	// bus shutdown
	// Sender connect attempt after termination
	ch, disconnectFn, active := b.SenderConnect()
	assrt.Nil(ch)
	assrt.Nil(disconnectFn)
	assrt.False(active)
	//	Receiver connect after bus termination
	assrt.False(func() bool { _, ok := <-b.ReceiverConnect(); return ok }())
}

func sendN(inst int, b *B) bool {
	bs, disconnect, active := b.SenderConnect()
	if !active {
		return false
	}
	go func() {
		defer disconnect()
		bs <- fmt.Sprintf("input:%d\n", inst)
	}()
	return true
}
func receive(r <-chan interface{}) (msgCnt int) {
	for m := range r {
		if _, ok := m.(string); ok {
			msgCnt++
		}
	}
	return msgCnt
}

type discard struct{}

func (discard) Write([]byte) (int, error)       { return 0, nil }
func (discard) WriteString(string) (int, error) { return 0, nil }

func Benchmark_Example(b *testing.B) {
	const msgMult = 100
	const msgRepl = 10
	fmt.Printf("Messages Sent Total: %d\n", 2*msgRepl*b.N*msgMult)
	for i := 0; i < b.N; i++ {
		var b B
		for sc := 0; sc < msgMult; sc++ {
			// these synchronous calls to construct and connect Senders
			// to the bus complete before allocating a Receiver to process
			// any Messages.  This enforces Happens Before constraint
			// that ensures all anticipated Senders connect to the bus
			// before it shuts down.
			senderCommand(&b, cmmdX{}, msgRepl)
			senderCommand(&b, cmmdY{}, msgRepl)
		}
		select {
		case <-b.ShutdownMonitor():
			panic("shutdown monitor should block")
		default:
		}
		quit := receiverCommands(&b, discard{})
		<-b.ShutdownMonitor()
		<-quit
	}
}

type cmmdX struct {
}

type cmmdY struct {
}

func senderCommand(b *B, msg interface{}, msgRepl int) {
	// this routine cannot be called as a goroutine as it
	// enforces a "Happens Before" constraint by immediately
	// allocating a sending connection before launching a goroutine.
	c, df, active := b.SenderConnect()
	if !active {
		panic("Bus should be active")
	}
	go func() {
		defer df()
		for i := 0; i < 10; i++ {
			c <- msg
		}
	}()
}
func receiverCommands(b *B, w io.Writer) <-chan struct{} {
	quit := make(chan struct{})
	m := b.ReceiverConnect()
	go func() {
		defer close(quit)
		ycnt := 0
		xcnt := 0
		for msg := range m {
			switch msg.(type) {
			case cmmdY:
				ycnt++
			case cmmdX:
				xcnt++
			default:
				panic("received unexpected command")

			}
		}
		io.WriteString(w, fmt.Sprintf("ycnt=%d\n", ycnt))
		io.WriteString(w, fmt.Sprintf("xcnt=%d\n", xcnt))
	}()
	return quit
}
