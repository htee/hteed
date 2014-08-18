package stream

import (
	"errors"
	"io"

	"github.com/htee/hteed/Godeps/_workspace/src/code.google.com/p/go.net/context"
	"github.com/htee/hteed/Godeps/_workspace/src/github.com/garyburd/redigo/redis"
)

func Out(ctx context.Context, name string, writer io.Writer) *Stream {
	s := newStream(ctx, name)

	go streamOut(s, writer)

	return s
}

func streamOut(s *Stream, writer io.Writer) {
	defer s.close()

	bufErrChan := make(chan bufErr)

	go subscribe(s, bufErrChan)

	for {
		select {
		case <-s.ctx.Done():
			return
		case v, ok := <-bufErrChan:
			if v.err == io.EOF || !ok {
				return
			} else if v.err != nil {
				s.Err = v.err
				return
			} else {
				if _, err := writer.Write(v.buf); err != nil {
					s.Err = err
					return
				}
			}
		}
	}
}

func subscribe(s *Stream, bufErrChan chan<- bufErr) {
	defer close(bufErrChan)

	state, buf, err := s.subscribe()

	bufErrChan <- bufErr{buf, err}

	if err == nil && state == Opened {
		streamChannel(s, bufErrChan)
	}
}

func streamChannel(s *Stream, bufErrChan chan<- bufErr) {
	psc := redis.PubSubConn{s.conn}

	for {
		switch v := psc.Receive().(type) {
		case redis.Message:
			state, buf := State(v.Data[0]), v.Data[1:]

			if state == Closed {
				return
			} else {
				bufErrChan <- bufErr{buf, nil}
			}
		case error:
			bufErrChan <- bufErr{nil, error(v)}

			return
		default:
			bufErrChan <- bufErr{nil, errors.New("Unrecognized redis message")}

			return
		}
	}
}
