package main

import (
	"bytes"
	"fmt"
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
					hj, _ := w.(http.Hijacker)
					conn, bufrw, _ := hj.Hijack()

					bufrw.WriteString("0\r\n\r\n")
					bufrw.Flush()
					conn.Close()

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

		if sse, ok := w.(sseResponse); ok {
			sse.Close()
		}
	}
}

func setupSSE(_ http.HandlerFunc, w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(200)
	w.Write([]byte(sse_shell))
}

type sseResponse struct {
	w http.ResponseWriter
}

func sseWriter(w http.ResponseWriter) sseResponse {
	return sseResponse{w}
}

func (r sseResponse) Header() http.Header {
	return r.w.Header()
}

func (r sseResponse) WriteHeader(status int) {
	r.w.WriteHeader(status)
}

func (r sseResponse) Flush() {
	r.w.(http.Flusher).Flush()
}

func (r sseResponse) Close() {
	r.writeCtrl("eof")
}

func (r sseResponse) Write(buf []byte) (int, error) {
	count := 0
	var err error = nil

	for len(buf) > 0 {
		if i := bytes.Index(buf, []byte("0\r\n\r\n")); i != -1 {
			count, err = r.writeData(buf[0:i], count)
			r.writeCtrl("eof")
			return count, err
		} else {
			if i := bytes.IndexAny(buf, "\r\n"); i == -1 {
				return r.writeData(buf, count)
			} else if i == 0 {
				c := buf[0]
				buf = buf[1:]

				if c == '\r' {
					if err = r.writeCtrl("carriage-return"); err != nil {
						return count, err
					}
				} else {
					if err = r.writeCtrl("newline"); err != nil {
						return count, err
					}
				}
			} else {
				count, err = r.writeData(buf[0:i], count)
				buf = buf[i:]
			}
		}
	}

	return count, err
}

func (r sseResponse) writeCtrl(ctrl string) error {
	_, err := r.writeSSE("ctrl", []byte(ctrl), 0)
	return err
}

func (r sseResponse) writeData(buf []byte, count int) (int, error) {
	return r.writeSSE("message", buf, count)
}

func (r sseResponse) writeSSE(event string, buf []byte, count int) (int, error) {
	if err := r.sendEvent(event); err != nil {
		return count, err
	}

	c, err := r.sendMessage(buf)

	return c + count, err
}

func (r sseResponse) sendEvent(event string) error {
	data := []byte(fmt.Sprintf("event: %s\n", event))

	_, err := r.w.Write(data)
	r.w.(http.Flusher).Flush()

	return err
}

func (r sseResponse) sendMessage(buf []byte) (int, error) {
	data := []byte(fmt.Sprintf("data: %s\n\n", buf))

	count, err := r.w.Write(data)
	r.w.(http.Flusher).Flush()

	return count - 8, err
}

func playbackSSE(h http.HandlerFunc, w http.ResponseWriter, r *http.Request) {
	hdr := w.Header()
	hdr.Set("Content-Type", "text/event-stream")
	hdr.Set("Cache-Control", "no-cache")
	hdr.Set("Connection", "close")

	h(sseWriter(w), r)
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
		rack.If(rack.IsStreamPath, rack.Stack{
			rack.If(rack.IsChunkedPost, rack.Stack{rack.Build(recordStream)}),
			rack.If(rack.IsSSE, rack.Stack{
				rack.Build(playbackSSE),
				rack.Build(playbackStream)}),
			rack.If(rack.IsBrowser, rack.Stack{rack.Build(setupSSE)}),
			rack.If(rack.IsGet, rack.Stack{rack.Build(playbackStream)}),
		})}

	rack.ListenAndServe(c.Addr, s)
}

const sse_shell = `
<!DOCTYPE html>
<html>
<head>
  <meta charset="utf-8" />
  <script type="text/javascript" src="https://ajax.googleapis.com/ajax/libs/jquery/1.10.1/jquery.min.js"></script>
</head>
<body><pre><code></code></pre>
  <script>
    var source = new EventSource(window.location.pathname);
    var body = $("html,body");
    var cursor = $("pre code")[0];
    var resetLine = false;

    function append(data) {
      if(resetLine == true) {
        cursor.innerHTML = data;
      } else {
        cursor.innerHTML += data;
      }

      resetLine = false;
    }

    source.onmessage = function(e) {
      append(e.data);
    };

    source.onerror = function(e) {
      console.log(e);
    };

    source.addEventListener('ctrl', function(e) {
      if(e.data == 'newline') {
        append("\n");
        cursor = $("<code></code>").insertAfter(cursor)[0];
      } else if(e.data == 'crlf' || e.data == 'carriage-return') {
        resetLine = true;
      } else if(e.data == 'eof') {
        source.close();
      } else {
        console.log(e);
      }
    }, false);

    window.setInterval(function(){
      window.scrollTo(0, document.body.scrollHeight);
    }, 100);

  </script>
</body>
</html>
`
