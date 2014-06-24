package main

import (
	"fmt"
	"io"
	"strings"

	"github.com/benburkert/http"
)

func recordStream(_ http.HandlerFunc, w http.ResponseWriter, r *http.Request) {
	owner, name := parseNWO(r.URL.Path)

	in := htee.StreamIn(owner, name)
	inc := in.In()

	defer in.Close()

	for {
		select {
		case err := <-in.Errors():
			fmt.Println(err.Error())
			w.WriteHeader(500)

			return
		default:
			buf := make([]byte, 4096)

			if n, err := r.Body.Read(buf); err == io.EOF || err == io.ErrUnexpectedEOF {
				w.WriteHeader(204)
				w.(http.Flusher).Flush()

				return
			} else if err != nil {
				fmt.Println(err.Error())
				w.WriteHeader(500)

				return
			} else {
				inc <- buf[:n]
			}
		}
	}
}

func playbackStream(_ http.HandlerFunc, w http.ResponseWriter, r *http.Request) {
	owner, name := parseNWO(r.URL.Path)

	responseStarted := false
	out := htee.StreamOut(owner, name)
	cc := w.(http.CloseNotifier).CloseNotify()

	defer out.Close()

	for {
		select {
		case err := <-out.Errors():
			fmt.Println(err.Error())

			if !responseStarted {
				w.WriteHeader(500)
			}

			return
		case <-cc:
			return
		case data, ok := <-out.Out():
			if ok {

				if !responseStarted {
					w.WriteHeader(200)
					responseStarted = true
				}

				w.Write(data)
				w.(http.Flusher).Flush()
			} else {
				hj, _ := w.(http.Hijacker)
				c, bufrw, _ := hj.Hijack()

				bufrw.WriteString("0\r\n\r\n")
				bufrw.Flush()
				c.Close()

				return
			}
		}
	}
}

func playbackSSE(h http.HandlerFunc, w http.ResponseWriter, r *http.Request) {
	hdr := w.Header()
	hdr.Set("Content-Type", "text/event-stream")
	hdr.Set("Cache-Control", "no-cache")
	hdr.Set("Connection", "close")

	h(htee.SSEWriter(w), r)
}

func parseNWO(path string) (owner, name string) {
	parts := strings.Split(path[1:], "/")
	owner, name = parts[0], parts[1]

	return
}

func main() {
	c := htee.Configure()

	s := htee.Stack{
		htee.If(htee.IsStreamPath, htee.Stack{
			htee.If(htee.IsChunkedPost, htee.Stack{htee.Build(recordStream)}),
			htee.If(htee.IsSSE, htee.Stack{
				htee.Build(playbackSSE),
				htee.Build(playbackStream)}),
			htee.If(htee.IsBrowser, htee.Stack{htee.Build(htee.SetupSSE)}),
			htee.If(htee.IsGet, htee.Stack{htee.Build(playbackStream)}),
		})}

	htee.ListenAndServe(c.Addr, s)
}
