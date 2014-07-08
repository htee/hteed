package htee

import (
	"fmt"
	"io"
	"net/http"
	"net/url"

	"github.com/codegangsta/negroni"
	"github.com/gorilla/mux"
)

var (
	upstream  *url.URL
	authToken string
)

func init() {
	ConfigCallback(configureServer)
}

func configureServer(cnf *ServerConfig) error {
	authToken = cnf.WebToken

	var err error
	upstream, err = url.Parse(cnf.WebURL)
	return err
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
	var body io.ReadCloser
	uu, err := s.u.Parse(r.URL.RequestURI())
	if err != nil {
		return nil, err
	}

	if len(r.TransferEncoding) == 0 || r.TransferEncoding[0] != "chunked" {
		body = r.Body
	}

	ur, err := http.NewRequest(r.Method, uu.String(), body)
	if err != nil {
		return nil, err
	}

	uh := http.Header{}
	for k, _ := range r.Header {
		uh.Set(k, r.Header.Get(k))
	}

	uh.Set("X-Forwarded-Host", r.Host)
	uh.Set("X-Htee-Authorization", "Token "+authToken)

	ur.Header = uh

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
