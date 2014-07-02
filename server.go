package htee

import (
	"fmt"
	"io"
	"net/http"
	"net/url"

	"strings"

	"github.com/codegangsta/negroni"
	"github.com/gorilla/mux"
)

var (
	upstream *url.URL
)

func init() {
	ConfigCallback(configureServer)
}

func configureServer(cnf *Config) {
	var err error
	if upstream, err = url.Parse(cnf.WebUrl); err != nil {
		panic(err)
	}
}

func ServerHandler() http.Handler {
	s := &server{
		r: mux.NewRouter(),
		t: &http.Transport{},
		u: upstream,
	}

	n := negroni.New()
	n.Use(negroni.HandlerFunc(s.upstreamMiddleware))

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

	n.UseHandler(s.r)

	return n
}

type server struct {
	r *mux.Router
	t *http.Transport

	u *url.URL
}

func (s *server) recordStream(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	owner := vars["owner"]
	name := vars["name"]

	if r.ExpectsContinue() {
		w.ContinueHeader().Set("Location", r.URL.Path)
	}

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

func (s *server) playbackStream(w http.ResponseWriter, r *http.Request) {
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

func (s *server) upstreamMiddleware(w http.ResponseWriter, r *http.Request, next http.HandlerFunc) {
	ur, err := s.proxyUpstream(r)
	if err != nil {
		fmt.Println(err.Error())
		w.WriteHeader(500)

		return
	} else if ur.StatusCode != 204 {
		if err = proxyResponse(w, ur); err != nil {
			fmt.Println(err.Error())
			w.WriteHeader(500)
		}

		return
	} else if ur.Header.Get("Location") != "" {
		loc, err := url.Parse(ur.Header.Get("Location"))
		if err != nil {
			fmt.Println(err.Error())
			w.WriteHeader(500)
		}

		if r.URL, err = r.URL.Parse(loc.Path); err != nil {
			fmt.Println(err.Error())
			w.WriteHeader(500)
		}
	}

	next(w, r)
}

func (s *server) proxyUpstream(r *http.Request) (*http.Response, error) {
	uu, err := s.u.Parse(r.URL.Path)
	if err != nil {
		return nil, err
	}

	ur := &http.Request{
		URL:    uu,
		Method: r.Method,
		Header: r.Header,
	}

	return s.t.RoundTrip(ur)
}

func proxyResponse(w http.ResponseWriter, r *http.Response) error {
	wh := w.Header()

	for k, v := range r.Header {
		wh[k] = v
	}

	w.WriteHeader(r.StatusCode)

	_, err := io.Copy(w, r.Body)
	return err
}

func isBrowser(r *http.Request, rm *mux.RouteMatch) bool {
	hdr := r.Header

	return strings.Contains(hdr.Get("User-Agent"), "WebKit") &&
		strings.Contains(hdr.Get("Accept"), "text/html")
}
