package htee

import (
	"fmt"
	"io"
	"net/http"

	"github.com/gorilla/mux"
)

func ServerHandler() http.Handler {
	r := mux.NewRouter()

	r.HandleFunc("/{owner}/{name}", recordStream).Methods("POST")
	r.HandleFunc("/{owner}/{name}", playbackStream).Methods("GET")

	return r
}

func recordStream(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	owner := vars["owner"]
	name := vars["name"]

	in := StreamIn(owner, name)
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
				inc <- buf[:n]

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

func playbackStream(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	owner := vars["owner"]
	name := vars["name"]

	responseStarted := false
	out := StreamOut(owner, name)
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
				return
			}
		}
	}
}
