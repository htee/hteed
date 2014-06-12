package main

import (
	"fmt"
	"io"
	"time"

	"github.com/benburkert/http"
	"github.com/garyburd/redigo/redis"
	"github.com/htio/htsd/config"
)

const (
	Streaming = iota
	Fin
)

var (
	pool *redis.Pool
)

func chunkedHandler(w http.ResponseWriter, r *http.Request) {
	buf := make([]byte, 4096)
	conn := pool.Get()

	defer conn.Close()

	var n int
	var err error

	stateKey := "state:" + r.URL.Path[1:]
	dataKey := "data:" + r.URL.Path[1:]
	streamKey := r.URL.Path[1:] // pub-sub channels don't show up in KEYS *

	conn.Do("SET", stateKey, Streaming)

	defer conn.Do("SET", stateKey, Fin)

	for {
		n, err = r.Body.Read(buf)

		if err != nil && err != io.EOF {
			fmt.Println(err.Error())
		}

		if n > 0 {
			conn.Send("MULTI")
			conn.Send("APPEND", dataKey, buf[:n])
			conn.Send("PUBLISH", streamKey, append([]byte{Streaming}, buf[:n]...))
			conn.Send("EXEC")
			conn.Flush()
			fmt.Printf("%s", buf[:n])
		} else {
			goto respond
		}
	}

respond:
	conn.Do("PUBLISH", streamKey, []byte{Fin})

	w.WriteHeader(204)
	fmt.Fprintf(w, "TransferEncoding: %s", r.TransferEncoding[0])
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

	_, err := conn.Do("PING")

	if err != nil {
		fmt.Println(err.Error())
	}

	http.HandleFunc("/", chunkedHandler)

	http.ListenAndServe(c.Addr, nil)
}
