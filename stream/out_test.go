package stream

import (
	"io"
	"net"
	"testing"

	"github.com/htee/hteed/Godeps/_workspace/src/code.google.com/p/go.net/context"
	"github.com/htee/hteed/Godeps/_workspace/src/github.com/garyburd/redigo/redis"
)

func TestCancelOut(t *testing.T) {
	_, w := io.Pipe()
	rConn, nConn := redisPipeConn()

	s := &Stream{
		conn: rConn,
		ctx:  context.Background(),
		Name: "canceled-out-stream",
		done: make(chan struct{}),
	}

	go streamOut(s, w)

	s.Cancel()

	if s.Err != nil {
		t.Error(s.Err)
	}

	_, err := nConn.Write([]byte("write on closed connection"))
	if err == nil {
		t.Error("Conn was not closed by Cancel()")
	}
}

func redisPipeConn() (redis.Conn, net.Conn) {
	nConn, c := net.Pipe()
	rConn := redis.NewConn(c, 0, 0)

	return rConn, nConn
}
