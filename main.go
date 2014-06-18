package main

import (
	"io"
	"time"

	"github.com/benburkert/http"
	"github.com/garyburd/redigo/redis"
	"github.com/htio/htsd/config"
	"github.com/htio/htsd/rack"
)

const (
	Streaming = iota
	Fin
)

var (
	pool *redis.Pool
)

func recordStream(_ http.HandlerFunc, w http.ResponseWriter, r *http.Request) {
	buf := make([]byte, 4096)
	conn := pool.Get()

	defer conn.Close()

	var n int
	var err error

	conn.Do("SET", stateKey(r), Streaming)

	defer conn.Do("SET", stateKey(r), Fin)

	for {
		n, err = r.Body.Read(buf)

		if err == io.EOF {
			conn.Send("PUBLISH", streamKey(r), []byte{Fin})
		} else if err != nil {
			w.WriteHeader(500)
			panic(err)
		}

		if n > 0 {
			conn.Send("MULTI")
			conn.Send("APPEND", dataKey(r), buf[:n])
			conn.Send("PUBLISH", streamKey(r), append([]byte{Streaming}, buf[:n]...))
			conn.Do("EXEC")
		} else {
			goto respond
		}
	}

respond:
	conn.Do("PUBLISH", streamKey, []byte{Fin})

	w.WriteHeader(204)
}

func playbackStream(_ http.HandlerFunc, w http.ResponseWriter, r *http.Request) {
	conn := pool.Get()
	defer conn.Close()

	state, err := redis.Int(conn.Do("GET", stateKey(r)))
	if err != nil {
		w.WriteHeader(500)
		panic(err)
	}

	switch state {
	case Streaming:
		psc := redis.PubSubConn{conn}

		conn.Send("MULTI")
		conn.Send("GET", dataKey(r))
		conn.Send("SUBSCRIBE", streamKey(r))
		if results, err := redis.Values(conn.Do("EXEC")); err != nil {
			w.WriteHeader(500)
			panic(err)
		} else {
			var buf []byte
			redis.Scan(results, &buf)

			w.WriteHeader(200)
			w.Write(buf)
			w.(http.Flusher).Flush()
		}

		for {
			switch v := psc.Receive().(type) {
			case redis.Message:
				switch v.Data[0] {
				case Streaming:
					w.Write(v.Data[1:])
					w.(http.Flusher).Flush()
				case Fin:
					w.(http.Flusher).Flush()
					return
				}
			case error:
				panic(v)
			}
		}

	case Fin:

		data, err := redis.String(conn.Do("GET", dataKey(r)))

		if err != nil {
			w.WriteHeader(500)
			panic(err)
		}

		w.WriteHeader(200)
		w.Write([]byte(data))
		w.(http.Flusher).Flush()

	}
}

func stateKey(r *http.Request) string {
	return "state:" + streamKey(r)
}

func dataKey(r *http.Request) string {
	return "data:" + streamKey(r)
}

func streamKey(r *http.Request) string {
	return r.URL.Path[1:] // pub-sub channels don't show up in KEYS *
}

func redisPool(url string) *redis.Pool {
	return &redis.Pool{
		Dial: func() (redis.Conn, error) {
			return redis.Dial("tcp", url)
		},
		TestOnBorrow: func(c redis.Conn, t time.Time) error {
			_, err := c.Do("PING")
			return err
		},
	}
}

func main() {
	c := config.New()

	pool = redisPool(c.RedisUrl)

	conn := pool.Get()

	if _, err := conn.Do("PING"); err != nil {
		panic(err)
	}

	s := rack.Stack{
		rack.If(rack.IsChunkedPost, rack.Stack{rack.Build(recordStream)}),
		rack.If(rack.IsGet, rack.Stack{rack.Build(playbackStream)}),
	}

	rack.ListenAndServe(c.Addr, s)
}
