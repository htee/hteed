package htee

import (
	"fmt"
	"io"
	"net/http"

	"strings"

	"github.com/gorilla/mux"
)

func ServerHandler() http.Handler {
	s := &server{r: mux.NewRouter()}

	s.r.HandleFunc("/{owner}/{name}", playbackSSEStream).
		Methods("GET").
		Headers("Accept", "text/event-stream")
	s.r.HandleFunc("/{owner}/{name}", sendSSEShell).
		Methods("GET").
		MatcherFunc(isBrowser)
	s.r.HandleFunc("/{owner}/{name}", s.recordStream).
		Methods("POST")
	s.r.HandleFunc("/{owner}/{name}", s.playbackStream).
		Methods("GET").Name("stream")

	return s.r
}

type server struct {
	r *mux.Router
}

func (s *server) recordStream(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	owner := vars["owner"]
	name := vars["name"]

	in := StreamIn(owner, name)
	inc := in.In()

	defer in.Close()

	if r.ExpectsContinue() {
		loc, err := s.r.Get("stream").URL("owner", owner, "name", name)
		if err != nil {
			fmt.Println(err.Error())
			w.WriteHeader(500)
		}

		w.ContinueHeader().Set("Location", loc.String())
	}

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

func (_ *server) playbackStream(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	owner := vars["owner"]
	name := vars["name"]

	responseStarted := false
	out := StreamOut(owner, name)
	cc := w.(http.CloseNotifier).CloseNotify()

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

func isBrowser(r *http.Request, rm *mux.RouteMatch) bool {
	hdr := r.Header

	return strings.Contains(hdr.Get("User-Agent"), "WebKit") &&
		strings.Contains(hdr.Get("Accept"), "text/html")
}
