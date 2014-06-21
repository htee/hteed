package stream

import (
	"errors"
	"io"

	"github.com/benburkert/http"
	"github.com/garyburd/redigo/redis"
)

type State byte

const (
	Opened State = iota
	Closed
)

func In(owner, name string, conn redis.Conn) (s Stream, err error) {
	s = Stream{owner, name, Closed, 0, conn}
	err = s.open()

	return
}

func Out(owner, name string, conn redis.Conn) (s Stream, err error) {
	s = Stream{owner, name, Closed, 0, conn}
	s.State, err = s.getState()

	return
}

type Stream struct {
	Owner, Name string
	State       State

	size int

	conn redis.Conn
}

func (s Stream) From(r io.Reader) (c int, err error) {
	buf := make([]byte, 4096)

	defer s.close()

	for {
		if n, err := r.Read(buf); err == io.EOF {
			s.write(buf[:n])
			s.finish()

			return n + c, nil
		} else if err != nil {
			return n + c, err
		} else {
			s.write(buf[:n])
			c += n
		}
	}
}

func (s Stream) To(w io.Writer) (int, error) {
	if s.State == Opened {
		return s.streamTo(w)
	} else {
		return s.getTo(w)
	}
}

func (s Stream) open() (err error) {
	_, err = s.conn.Do("SET", s.stateKey(), Opened)

	return err
}

func (s Stream) close() (err error) {
	_, err = s.conn.Do("SET", s.stateKey(), Closed)

	s.conn.Close()

	return
}

func (s Stream) write(buf []byte) (err error) {
	s.conn.Send("MULTI")
	s.conn.Send("APPEND", s.dataKey(), buf)
	s.conn.Send("PUBLISH", s.streamKey(), append([]byte{byte(Opened)}, buf...))
	_, err = s.conn.Do("EXEC")

	return
}

func (s Stream) finish() (err error) {
	_, err = s.conn.Do("PUBLISH", s.streamKey(), []byte{byte(Closed)})

	return
}

func (s Stream) stateKey() string {
	return "state:" + s.nwo()
}

func (s Stream) dataKey() string {
	return "data:" + s.nwo()
}

func (s Stream) streamKey() string {
	return s.nwo()
}

func (s Stream) nwo() string {
	return s.Owner + "/" + s.Name
}

func (s Stream) getState() (State, error) {
	state, err := redis.Int(s.conn.Do("GET", s.stateKey()))

	return State(state), err
}

func (s Stream) streamTo(w io.Writer) (int, error) {
	if buf, err := s.getAndSubscribe(); err != nil {
		return len(buf), err
	} else {
		if n, err := w.Write(buf); err != nil {
			return n, err
		} else {
			w.(http.Flusher).Flush() // hack
			m, err := s.streamChannel(w)

			return n + m, err
		}
	}
}

func (s Stream) getAndSubscribe() (buf []byte, err error) {
	s.conn.Send("MULTI")
	s.conn.Send("GET", s.dataKey())
	s.conn.Send("SUBSCRIBE", s.streamKey())

	if data, err := redis.Values(s.conn.Do("EXEC")); err != nil {
		return buf, err
	} else {
		redis.Scan(data, &buf)

		return buf, nil
	}
}

func (s Stream) getTo(w io.Writer) (int, error) {
	if data, err := redis.String(s.conn.Do("GET", s.dataKey())); err != nil {
		return 0, err
	} else {
		w.Write([]byte(data))
		w.(http.Flusher).Flush() // hack

		return len(data), nil
	}
}

func (s Stream) streamChannel(w io.Writer) (n int, err error) {
	psc := redis.PubSubConn{s.conn}

	for {
		switch v := psc.Receive().(type) {
		case redis.Message:
			state, data := State(v.Data[0]), v.Data[1:]

			switch state {
			case Opened:
				n += len(data)

				w.Write(data)
				w.(http.Flusher).Flush() // hack
			case Closed:
				return
			}
		case error:
			err = error(v)

			return
		default:
			return n, errors.New("Unrecognized redis message")
		}
	}
}
