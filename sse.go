package htee

import (
	"fmt"
	"net/http"
	"strconv"

	"github.com/gorilla/mux"
)

const MaxDataSize = 4096 // 4KB

func playbackSSEStream(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	owner := vars["owner"]
	name := vars["name"]

	responseStarted := false
	out := StreamOut(owner, name)
	sseOut := formatSSEData(out.Out())
	cc := w.(http.CloseNotifier).CloseNotify()

	hdr := w.Header()
	hdr.Set("Content-Type", "text/event-stream")
	hdr.Set("Cache-Control", "no-cache")
	hdr.Set("Connection", "close")

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
		case data, ok := <-sseOut:
			if ok {

				if !responseStarted {
					w.WriteHeader(200)
					responseStarted = true
				}

				w.Write([]byte(data))
				w.(http.Flusher).Flush()
			} else {
				w.Write([]byte("event:eof\ndata:\n\n"))
				w.(http.Flusher).Flush()
				return
			}
		}
	}
}

func formatSSEData(uc <-chan []byte) <-chan string {
	ec := make(chan string)

	go func() {
		for buf := range uc {
			for i, n := 0, min(MaxDataSize, len(buf)); i < len(buf); i += MaxDataSize {
				ec <- "data:" + strconv.Quote(string(buf[i:n])) + "\n\n"

				n = min(n+MaxDataSize, len(buf))
			}
		}

		close(ec)
	}()

	return ec
}

func min(a, b int) int {
	if a < b {
		return a
	} else {
		return b
	}
}
