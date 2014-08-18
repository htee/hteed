package stream

import (
	"io"

	"github.com/htee/hteed/Godeps/_workspace/src/code.google.com/p/go.net/context"
)

func In(ctx context.Context, name string, reader io.Reader) *Stream {
	s := newStream(ctx, name)

	go streamIn(s, reader)

	return s
}

func streamIn(s *Stream, reader io.Reader) {
	defer closeIn(s)

	bufErrChan := make(chan bufErr)

	go drain(bufErrChan, reader)

	for {
		select {
		case <-s.ctx.Done():
			return
		case v, ok := <-bufErrChan:
			if v.err == io.EOF || v.err == io.ErrUnexpectedEOF || !ok {
				return
			} else if v.err != nil {
				s.Err = v.err
				return
			} else {
				if err := s.append(v.buf); err != nil {
					s.Err = err
					return
				}
			}
		}
	}
}

func drain(bufErrChan chan<- bufErr, reader io.Reader) {
	defer close(bufErrChan)

	for {
		buf := make([]byte, 4096)

		if n, err := reader.Read(buf); err != nil {
			if n > 0 {
				bufErrChan <- bufErr{buf[:n], nil}
			}

			bufErrChan <- bufErr{nil, err}
			return
		} else {
			bufErrChan <- bufErr{buf[:n], nil}
		}
	}
}

func closeIn(s *Stream) {
	defer s.close()

	if err := s.finish(); err != nil {
		s.Err = err
	}
}
