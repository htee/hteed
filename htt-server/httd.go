package main

import (
	"fmt"
	"io"
	"strings"

	"github.com/benburkert/htt"
	"github.com/benburkert/http"
)

func recordStream(_ http.HandlerFunc, w http.ResponseWriter, r *http.Request) {
	owner, name := parseNWO(r.URL.Path)

	in := htt.StreamIn(owner, name)

	defer in.Close()

	for {
		select {
		case err := <-in.Errors():
			fmt.Println(err.Error())
			w.WriteHeader(500)

			return
		default:
			buf := make([]byte, 4096)

			if n, err := r.Body.Read(buf); err == io.EOF {
				w.WriteHeader(204)
				w.(http.Flusher).Flush()
				close(in.In())

				return
			} else if err != nil {
				fmt.Println(err.Error())
				w.WriteHeader(500)

				return
			} else {
				in.In() <- buf[:n]
			}
		}
	}
}

func playbackStream(_ http.HandlerFunc, w http.ResponseWriter, r *http.Request) {
	owner, name := parseNWO(r.URL.Path)

	responseStarted := false
	out := htt.StreamOut(owner, name)

	defer out.Close()

	for {
		select {
		case err := <-out.Errors():
			fmt.Println(err.Error())

			if !responseStarted {
				w.WriteHeader(500)
			}

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

	h(htt.SSEWriter(w), r)
}

func parseNWO(path string) (owner, name string) {
	parts := strings.Split(path[1:], "/")
	owner, name = parts[0], parts[1]

	return
}

func main() {
	c := htt.Configure()

	s := htt.Stack{
		htt.If(htt.IsStreamPath, htt.Stack{
			htt.If(htt.IsChunkedPost, htt.Stack{htt.Build(recordStream)}),
			htt.If(htt.IsSSE, htt.Stack{
				htt.Build(playbackSSE),
				htt.Build(playbackStream)}),
			htt.If(htt.IsBrowser, htt.Stack{htt.Build(htt.SetupSSE)}),
			htt.If(htt.IsGet, htt.Stack{htt.Build(playbackStream)}),
		})}

	htt.ListenAndServe(c.Addr, s)
}
