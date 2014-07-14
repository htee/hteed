package htee

import (
	"encoding/json"
	"errors"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/codegangsta/negroni"
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
		transport: &http.Transport{
			ResponseHeaderTimeout: 5 * time.Second,
		},
		upstream: upstream,
		logger:   log.New(os.Stdout, "[server] ", log.LstdFlags),
	}

	n := negroni.New()
	n.Use(negroni.HandlerFunc(s.upstreamMiddleware))

	n.UseHandler(http.HandlerFunc(s.topHandler))

	return n
}

type server struct {
	logger    *log.Logger
	transport *http.Transport
	upstream  *url.URL
}

func (s *server) topHandler(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case "GET":
		s.playbackStream(w, r)
	case "POST":
		s.recordStream(w, r)
	case "DELETE":
		s.deleteStream(w, r)
	}
}

func (s *server) recordStream(w http.ResponseWriter, r *http.Request) {
	name := r.URL.Path[1:]

	hj, ok := w.(http.Hijacker)
	if !ok {
		s.handleError(w, r, errors.New("webserver doesn't support hijacking"))
		return
	}

	conn, bufrw, err := hj.Hijack()
	if err != nil {
		s.handleError(w, r, err)
	}

	defer conn.Close()

	bufrw.WriteString("HTTP/1.1 100 Continue\r\n")
	bufrw.WriteString("Location: " + r.URL.Path + "\r\n\r\n")
	bufrw.Flush()

	in := StreamIn(name)
	inc := in.In()

	defer in.Close()

	for {
		select {
		case err := <-in.Errors():
			s.handleError(w, r, err)

			return
		default:
			buf := make([]byte, 4096)

			if n, err := r.Body.Read(buf); err == io.EOF || err == io.ErrUnexpectedEOF {
				inc <- buf[:n]

				bufrw.WriteString("HTTP/1.1 204 No Content\r\n")
				bufrw.WriteString("Date: " + time.Now().UTC().Format(time.RFC1123) + "\r\n")
				bufrw.WriteString("Connection: close\r\n\r\n")
				bufrw.Flush()

				return
			} else if err != nil {
				s.handleError(w, r, err)

				return
			} else {
				inc <- buf[:n]
			}
		}
	}
}

func (s *server) deleteStream(w http.ResponseWriter, r *http.Request) {
	name := r.URL.Path[1:]

	if err := StreamDelete(name); err != nil {
		s.handleError(w, r, err)
	} else {
		w.WriteHeader(204)
		w.(http.Flusher).Flush()
	}
}

func (s *server) playbackStream(w http.ResponseWriter, r *http.Request) {
	name := r.URL.Path[1:]

	out := StreamOut(name)
	cc := w.(http.CloseNotifier).CloseNotify()

	write := w.Write
	if isSSE(r) {
		w.Header().Set("Content-Type", "text/event-stream")
		write = sseWriter{w}.Write
	}

	w.WriteHeader(200)

	for {
		select {
		case err := <-out.Errors():
			s.handleError(w, r, err)
			return
		case <-cc:
			return
		case data, ok := <-out.Out():
			if ok {
				write(data)
				w.(http.Flusher).Flush()
			} else {
				return
			}
		}
	}
}

func (s *server) handleError(w http.ResponseWriter, r *http.Request, err error) {
	s.logger.Printf("%s - ERROR: %s", r.RemoteAddr, err.Error())
	http.Error(w, err.Error(), http.StatusInternalServerError)
}

func (s *server) upstreamMiddleware(w http.ResponseWriter, r *http.Request, next http.HandlerFunc) {
	ur, err := s.proxyUpstream(r)

	if err != nil {
		s.handleError(w, r, err)
		return
	} else if ur.StatusCode == 202 {
		if strings.Contains(ur.Header.Get("Content-Type"), "application/json") {
			var message struct {
				Method, Path, Body string
				Headers            map[string]string
			}

			dec := json.NewDecoder(ur.Body)
			if err = dec.Decode(&message); err != nil {
				s.handleError(w, r, err)
				return
			}

			if message.Body != "" {
				r.Method = message.Method
			}

			if message.Path != "" {
				if r.URL, err = r.URL.Parse(message.Path); err != nil {
					s.handleError(w, r, err)
					return
				}
			}

			for k, v := range message.Headers {
				r.Header.Set(k, v)
			}

			if message.Body != "" {
				r.Body = ioutil.NopCloser(strings.NewReader(message.Body))
			}
		}
	} else if ur.StatusCode != 204 {
		if err = proxyResponse(w, ur); err != nil {
			s.handleError(w, r, err)
			return
		}

		return
	}

	next(w, r)
}

func (s *server) proxyUpstream(r *http.Request) (*http.Response, error) {
	var body io.ReadCloser
	uu, err := s.upstream.Parse(r.URL.RequestURI())
	if err != nil {
		return nil, err
	}

	if len(r.TransferEncoding) == 0 || r.TransferEncoding[0] != "chunked" {
		body = r.Body
		r.Body = ioutil.NopCloser(strings.NewReader(""))
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

	return s.transport.RoundTrip(ur)
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
