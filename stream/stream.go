package stream

import (
	"errors"
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

func StreamDelete(ctx context.Context, name string) error {
	return newStream(ctx, name).delete()
}

func newStream(ctx context.Context, name string) *stream {
	return &stream{
		ctx:  ctx,
		name: name,
		conn: pool.Get(),
		done: make(chan struct{}),
	}
}

type Stream interface {
	Name() string

	Cancel()
	Done() <-chan struct{}
	Err() error
}

type stream struct {
	ctx context.Context

	name string

	conn redis.Conn

	err error

	done chan struct{}
}

func (s *stream) Name() string { return s.name }

func (s *stream) Done() <-chan struct{} { return s.done }

func (s *stream) Err() error { return s.err }

func (s *stream) Cancel() { panic("TODO") }

func (s *stream) close() {
	close(s.done)
	s.conn.Close()
}

type bufErr struct {
	buf []byte
	err error
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

func (s *stream) subscribe() (state State, buf []byte, err error) {
	s.conn.Send("MULTI")
	s.conn.Send("GET", s.stateKey())
	s.conn.Send("GET", s.dataKey())
	s.conn.Send("SUBSCRIBE", s.streamKey())

	if data, err := redis.Values(s.conn.Do("EXEC")); err == nil {
		_, err = redis.Scan(data, &state, &buf)
	}

	return
}

func (s *stream) finish() error {
	s.conn.Send("MULTI")
	s.conn.Send("SET", s.stateKey(), Closed)
	s.conn.Send("PUBLISH", s.streamKey(), []byte{byte(Closed)})
	_, err := s.conn.Do("EXEC")

	return err
}

func (s *stream) getState() (State, error) {
	state, err := redis.Int(s.conn.Do("GET", s.stateKey()))

	return State(state), err
}

func (s *stream) stateKey() string { return keyPrefix + "state:" + s.name }

func (s *stream) dataKey() string { return keyPrefix + "data:" + s.name }

func (s *stream) streamKey() string { return keyPrefix + s.name }
