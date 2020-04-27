package bus_test

import (
	"fmt"
	"io"
	"os"

	"github.com/WhisperingChaos/bus"
)

func ExampleB() {
	var b bus.B
	senderCommand(&b, cmmdX{}, 10)
	senderCommand(&b, cmmdY{}, 10)
	select {
	case <-b.ShutdownMonitor():
		panic("active senders - shutdown monitor should block")
	default:
	}
	// senderCommand connects senders to
	// the bus before (Happens Before) connecting
	// a receiver.  All senders are ready before
	// connecting a receiver.  This coupled with
	// blocking senders, avoids situation
	// where a participating sender attaches after
	// bus shutdown.
	quit := receiverCommands(&b, os.Stdout)
	// senders terminated - bus should be in shutdown state
	//
	<-b.ShutdownMonitor()
	<-quit
	// Output:
	// ycnt=10
	// xcnt=10
}

type cmmdX struct {
}

type cmmdY struct {
}

func senderCommand(b *bus.B, msg interface{}, msgRepl int) {
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
func receiverCommands(b *bus.B, w io.Writer) <-chan struct{} {
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
