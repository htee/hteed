package htee

import (
	"errors"
	"time"

	"github.com/garyburd/redigo/redis"
)

type State byte

const (
	Opened State = iota
	Closed
)

var (
	pool      *redis.Pool
	keyPrefix string
)

func init() {
	ConfigCallback(configureStream)
}

func configureStream(cnf *Config) {
	pool = &redis.Pool{
		Dial: func() (redis.Conn, error) {
			return redis.Dial("tcp", cnf.RedisUrl)
		},
		TestOnBorrow: func(c redis.Conn, t time.Time) error {
			_, err := c.Do("PING")
			return err
		},
	}

	conn := pool.Get()
	defer conn.Close()

	if _, err := conn.Do("PING"); err != nil {
		panic(err)
	}

	keyPrefix = cnf.KeyPrefix
}

func StreamIn(owner, name string) InStream {
	s := newStream(owner, name)
	s.open()

	go s.streamIn()

	return s
}

func StreamOut(owner, name string) OutStream {
	s := newStream(owner, name)

	go s.streamOut()

	return s
}

func newStream(owner, name string) *stream {
	return &stream{
		owner:  owner,
		name:   name,
		conn:   pool.Get(),
		data:   make(chan []byte),
		err:    make(chan error),
		closed: false,
	}
}

type Stream interface {
	Owner() string
	Name() string

	Close()
	Errors() <-chan error
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
	owner, name string

	conn redis.Conn

	data chan []byte

	err chan error

	closed bool
}

func (s *stream) Owner() string { return s.owner }

func (s *stream) Name() string { return s.name }

func (s *stream) Errors() <-chan error { return s.err }

func (s *stream) In() chan<- []byte { return s.data }

func (s *stream) Out() <-chan []byte { return s.data }

func (s *stream) Close() {
	if s.closed {
		return
	}

	s.closed = true
	close(s.data)
}

func (s *stream) open() {
	if _, err := s.conn.Do("SET", s.stateKey(), Opened); err != nil {
		s.err <- err
	}
}

func (s *stream) streamIn() {
	defer s.conn.Close()

	for {
		select {
		case buf, ok := <-s.data:
			if ok {
				s.append(buf)
			} else {
				s.closeIn()
				return
			}
		}
	}
}

func (s *stream) streamOut() {
	defer s.conn.Close()
	defer s.Close()

	var state State
	var buf []byte

	s.conn.Send("MULTI")
	s.conn.Send("GET", s.stateKey())
	s.conn.Send("GET", s.dataKey())
	s.conn.Send("SUBSCRIBE", s.streamKey())

	if data, err := redis.Values(s.conn.Do("EXEC")); err != nil {
		s.err <- err
	} else {
		redis.Scan(data, &state, &buf)

		s.data <- buf

		if state == Opened {
			s.streamChannel()
		}
	}
}

func (s *stream) append(buf []byte) {
	s.conn.Send("MULTI")
	s.conn.Send("SET", s.stateKey(), Opened)
	s.conn.Send("APPEND", s.dataKey(), buf)
	s.conn.Send("PUBLISH", s.streamKey(), append([]byte{byte(Opened)}, buf...))
	if _, err := s.conn.Do("EXEC"); err != nil {
		s.err <- err
	}
}

func (s *stream) closeIn() {
	s.conn.Send("MULTI")
	s.conn.Send("SET", s.stateKey(), Closed)
	s.conn.Send("PUBLISH", s.streamKey(), []byte{byte(Closed)})
	if _, err := s.conn.Do("EXEC"); err != nil {
		s.err <- err
	}
}

func (s *stream) streamChannel() {
	psc := redis.PubSubConn{s.conn}

	for {
		if s.closed {
			return
		}

		switch v := psc.Receive().(type) {
		case redis.Message:
			state, data := State(v.Data[0]), v.Data[1:]

			if state == Closed {
				return
			} else {
				s.data <- data
			}
		case error:
			s.err <- error(v)

			return
		default:
			s.err <- errors.New("Unrecognized redis message")

			return
		}
	}
}

func (s *stream) getState() (State, error) {
	state, err := redis.Int(s.conn.Do("GET", s.stateKey()))

	return State(state), err
}

func (s *stream) stateKey() string { return keyPrefix + "state:" + s.nwo() }

func (s *stream) dataKey() string { return keyPrefix + "data:" + s.nwo() }

func (s *stream) streamKey() string { return keyPrefix + s.nwo() }

func (s *stream) nwo() string { return s.owner + "/" + s.name }

func delTestData() {
	conn := pool.Get()

	keys, err := redis.Strings(conn.Do("KEYS", keyPrefix+"*"))
	if err != nil {
		panic(err)
	}

	for _, key := range keys {
		conn.Do("DEL", key)
	}
}
