package main

import (
	"fmt"
	"io"
	"time"

	"github.com/benburkert/http"
	"github.com/garyburd/redigo/redis"
	"github.com/htio/htsd/config"
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

	conn.Do("SET", stateKey, "STREAMING")

	defer conn.Do("SET", stateKey, "FIN")

	for {
		n, err = r.Body.Read(buf)

		if err != nil && err != io.EOF {
			fmt.Println(err.Error())
		}

		if n > 0 {
			conn.Do("APPEND", dataKey, buf[:n])
			fmt.Printf("%s", buf[:n])
		} else {
			goto respond
		}
	}

respond:
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
