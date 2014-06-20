package stream

import (
	"io"

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

	return s, err
}

type Stream struct {
	Owner, Name string
	State       State

	size int

	conn redis.Conn
}

func (s Stream) Fill(r io.Reader) (c int, err error) {
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
