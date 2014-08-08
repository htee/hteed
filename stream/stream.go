package stream

import (
	"errors"
	"io"
	"time"

	"code.google.com/p/go.net/context"

	"github.com/htee/hteed/Godeps/_workspace/src/github.com/garyburd/redigo/redis"
	"github.com/htee/hteed/config"
)

type State byte

const (
	Closed State = iota
	Opened
)

var (
	pool      *redis.Pool
	keyPrefix string
	testing   bool
)

func init() {
	config.ConfigCallback(configureStream)
}

func configureStream(cnf *config.Config) error {
	pool = &redis.Pool{
		Dial: func() (redis.Conn, error) {
			return redis.Dial("tcp", cnf.RedisURL)
		},
		TestOnBorrow: func(c redis.Conn, t time.Time) error {
			_, err := c.Do("PING")
			return err
		},
	}

	conn := pool.Get()
	defer conn.Close()

	if _, err := conn.Do("PING"); err != nil {
		return err
	}

	keyPrefix = cnf.KeyPrefix
	testing = cnf.Testing

	return nil
}

func Reset() error {
	if !testing {
		return errors.New("Reset disabled unless testing")
	}

	if keyPrefix == "" {
		return errors.New("Reset requires a key prefix")
	}

	conn := pool.Get()

	keys, err := redis.Strings(conn.Do("KEYS", keyPrefix+"*"))
	if err != nil {
		return err
	}

	for _, key := range keys {
		conn.Do("DEL", key)
	}

	return nil
}

func In(ctx context.Context, name string, reader io.Reader) InStream {
	s := newStream(ctx, name)

	go s.streamIn(reader)

	return s
}

func Out(ctx context.Context, name string, writer io.Writer) OutStream {
	s := newStream(ctx, name)

	go s.streamOut(writer)

	return s
}

func StreamDelete(ctx context.Context, name string) error {
	return newStream(ctx, name).delete()
}

func newStream(ctx context.Context, name string) *stream {
	return &stream{
		ctx:    ctx,
		name:   name,
		conn:   pool.Get(),
		data:   make(chan []byte),
		done:   make(chan struct{}),
		closed: false,
	}
}

type Stream interface {
	Name() string

	Cancel()
	Done() <-chan struct{}
	Err() error
}

type InStream interface {
	Stream

	In() chan<- []byte
}

type OutStream interface {
	Stream

	Out() <-chan []byte
}

type stream struct {
	ctx context.Context

	name string

	conn redis.Conn

	data chan []byte

	err error

	done chan struct{}

	closed bool
}

func (s *stream) Name() string { return s.name }

func (s *stream) Done() <-chan struct{} { return s.done }

func (s *stream) Err() error { return s.err }

func (s *stream) In() chan<- []byte { return s.data }

func (s *stream) Out() <-chan []byte { return s.data }

func (s *stream) Cancel() { panic("TODO") }

func (s *stream) close() {
	if s.closed {
		panic("Close() called twice")
	}

	s.closed = true
	close(s.data)
	close(s.done)
	s.conn.Close()
}

type bufErr struct {
	buf []byte
	err error
}

func (s *stream) streamIn(reader io.Reader) {
	defer s.closeIn()

	bufErrChan := s.drain(reader)

	for {
		select {
		case <-s.ctx.Done():
			return
		case v, ok := <-bufErrChan:
			if v.err == io.EOF || v.err == io.ErrUnexpectedEOF || !ok {
				return
			} else if v.err != nil {
				s.err = v.err
				return
			} else {
				if err := s.append(v.buf); err != nil {
					s.err = err
					return
				}
			}
		}
	}
}

func (s *stream) drain(reader io.Reader) <-chan bufErr {
	bufErrChan := make(chan bufErr)

	go func() {
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
	}()

	return bufErrChan
}

func (s *stream) streamOut(writer io.Writer) {
	defer s.close()

	bufErrChan := make(chan bufErr)

	go s.subscribe(bufErrChan)

	for {
		select {
		case <-s.ctx.Done():
			return
		case v, ok := <-bufErrChan:
			if v.err == io.EOF || !ok {
				return
			} else if v.err != nil {
				s.err = v.err
				return
			} else {
				if _, err := writer.Write(v.buf); err != nil {
					s.err = err
					return
				}
			}
		}
	}
}

func (s *stream) subscribe(bufErrChan chan<- bufErr) {
	defer close(bufErrChan)

	var (
		state State
		buf   []byte
	)

	s.conn.Send("MULTI")
	s.conn.Send("GET", s.stateKey())
	s.conn.Send("GET", s.dataKey())
	s.conn.Send("SUBSCRIBE", s.streamKey())

	if data, err := redis.Values(s.conn.Do("EXEC")); err != nil {
		bufErrChan <- bufErr{nil, err}
	} else {
		redis.Scan(data, &state, &buf)

		bufErrChan <- bufErr{buf, nil}

		if state == Opened {
			s.streamChannel(bufErrChan)
		}
	}
}

func (s *stream) streamChannel(bufErrChan chan<- bufErr) {
	psc := redis.PubSubConn{s.conn}

	for {
		if s.closed {
			return
		}

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

func (s *stream) delete() error {
	s.conn.Send("MULTI")
	s.conn.Send("DEL", s.stateKey(), s.dataKey())
	s.conn.Send("PUBLISH", s.streamKey(), []byte{byte(Closed)})
	_, err := s.conn.Do("EXEC")

	return err
}

func (s *stream) append(buf []byte) error {
	s.conn.Send("MULTI")
	s.conn.Send("SET", s.stateKey(), Opened)
	s.conn.Send("APPEND", s.dataKey(), buf)
	s.conn.Send("PUBLISH", s.streamKey(), append([]byte{byte(Opened)}, buf...))
	_, err := s.conn.Do("EXEC")

	return err
}

func (s *stream) closeIn() {
	defer s.close()

	s.conn.Send("MULTI")
	s.conn.Send("SET", s.stateKey(), Closed)
	s.conn.Send("PUBLISH", s.streamKey(), []byte{byte(Closed)})
	if _, err := s.conn.Do("EXEC"); err != nil {
		s.err = err
	}
}

func (s *stream) getState() (State, error) {
	state, err := redis.Int(s.conn.Do("GET", s.stateKey()))

	return State(state), err
}

func (s *stream) stateKey() string { return keyPrefix + "state:" + s.name }

func (s *stream) dataKey() string { return keyPrefix + "data:" + s.name }

func (s *stream) streamKey() string { return keyPrefix + s.name }
