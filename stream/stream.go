package stream

import (
	"errors"

	"github.com/garyburd/redigo/redis"
)

type State byte

const (
	Opened State = iota
	Closed
)

func In(owner, name string, conn redis.Conn) InStream {
	s := stream{owner, name, conn, make(chan []byte), make(chan bool), make(chan error)}

	go s.streamIn()

	return s
}

func Out(owner, name string, conn redis.Conn) OutStream {
	s := stream{owner, name, conn, make(chan []byte), make(chan bool), make(chan error)}

	go s.streamOut()

	return s
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

	fin chan bool
	err chan error
}

func (s stream) Owner() string { return s.owner }

func (s stream) Name() string { return s.name }

func (s stream) Close() { s.fin <- true }

func (s stream) Errors() <-chan error { return s.err }

func (s stream) In() chan<- []byte { return s.data }

func (s stream) Out() <-chan []byte { return s.data }

func (s stream) streamIn() {
	defer s.conn.Close()

	for {
		select {
		case buf, ok := <-s.data:
			if ok {
				s.append(buf)
			} else {
				s.close()
				return
			}
		case <-s.fin:
			s.close()
			return
		}
	}
}

func (s stream) streamOut() {
	defer s.conn.Close()

	if state, err := s.getState(); err != nil {
		s.err <- err
	} else if state == Opened {
		s.streamData()
	} else {
		s.sendData()
	}
}

func (s stream) append(buf []byte) {
	s.conn.Send("MULTI")
	s.conn.Send("SET", s.stateKey(), Opened)
	s.conn.Send("APPEND", s.dataKey(), buf)
	s.conn.Send("PUBLISH", s.streamKey(), append([]byte{byte(Opened)}, buf...))
	if _, err := s.conn.Do("EXEC"); err != nil {
		s.err <- err
	}
}

func (s stream) close() {
	s.conn.Send("MULTI")
	s.conn.Send("SET", s.stateKey(), Closed)
	s.conn.Send("PUBLISH", s.streamKey(), []byte{byte(Closed)})
	if _, err := s.conn.Do("EXEC"); err != nil {
		s.err <- err
	}
}

func (s stream) sendData() {
	if data, err := redis.String(s.conn.Do("GET", s.dataKey())); err != nil {
		s.err <- err
	} else {
		s.data <- []byte(data)
	}
}

func (s stream) streamData() {
	var buf []byte

	s.conn.Send("MULTI")
	s.conn.Send("GET", s.dataKey())
	s.conn.Send("SUBSCRIBE", s.streamKey())

	if data, err := redis.Values(s.conn.Do("EXEC")); err != nil {
		s.err <- err
	} else {
		redis.Scan(data, &buf)

		s.data <- buf

		s.streamChannel()
	}
}

func (s stream) streamChannel() {
	psc := redis.PubSubConn{s.conn}

	for {
		switch v := psc.Receive().(type) {
		case redis.Message:
			state, data := State(v.Data[0]), v.Data[1:]

			if state == Closed {
				close(s.data)
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

func (s stream) getState() (State, error) {
	state, err := redis.Int(s.conn.Do("GET", s.stateKey()))

	return State(state), err
}

func (s stream) stateKey() string { return "state:" + s.nwo() }

func (s stream) dataKey() string { return "data:" + s.nwo() }

func (s stream) streamKey() string { return s.nwo() }

func (s stream) nwo() string { return s.owner + "/" + s.name }
