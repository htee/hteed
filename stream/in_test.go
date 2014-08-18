package stream

import (
	"io"
	"testing"

	"github.com/htee/hteed/Godeps/_workspace/src/code.google.com/p/go.net/context"
)

func TestCancelIn(t *testing.T) {
	r, _ := io.Pipe()
	rConn, nConn := redisPipeConn()

	s := &Stream{
		conn: rConn,
		ctx:  context.Background(),
		Name: "canceled-in-stream",
		done: make(chan struct{}),
	}

	go streamIn(s, r)

	s.Cancel()

	if s.Err != nil {
		t.Error(s.Err)
	}

	_, err := nConn.Read([]byte{})
	if err == nil {
		t.Error("Conn was not closed by Cancel()")
	}
}
