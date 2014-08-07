package server

import (
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"time"

	"code.google.com/p/go.net/context"

	"github.com/htee/hteed/Godeps/_workspace/src/github.com/codegangsta/negroni"
	"github.com/htee/hteed/config"
	"github.com/htee/hteed/stream"
)

var (
	upstream   *url.URL
	authHeader string
)

func init() {
	config.ConfigCallback(configureServer)
}

func configureServer(cnf *config.Config) error {
	authHeader = "Token " + cnf.WebToken

	var err error
	upstream, err = url.Parse(cnf.WebURL)
	if err != nil {
		return err
	}

	pingURL, err := upstream.Parse("/ping")
	if err != nil {
		return err
	}

	pingReq, err := http.NewRequest("GET", pingURL.String(), nil)
	if err != nil {
		return err
	}

	pingReq.Header.Set("X-Htee-Authorization", authHeader)
	pingClient := http.Client{
		Timeout: 1 * time.Second,
	}

	pingRes, err := pingClient.Do(pingReq)
	if err != nil {
		return err
	}

	if pingRes.StatusCode != 200 {
		return fmt.Errorf("Expected 200 OK response from upstream ping, got %s", pingRes.Status)
	}

	return err
}

func ServerHandler() http.Handler {
	s := &server{
		transport: &http.Transport{
			ResponseHeaderTimeout: 15 * time.Second,
		},
		upstream: upstream,
		logger:   log.New(os.Stdout, "[server] ", log.LstdFlags),
	}

	n := negroni.New()
	n.Use(negroni.HandlerFunc(s.upstreamMiddleware))
	n.Use(negroni.HandlerFunc(s.fixRailsVerbMiddleware))

	n.UseHandler(http.HandlerFunc(s.handleRequest))

	return n
}

type server struct {
	logger    *log.Logger
	transport *http.Transport
	upstream  *url.URL
}

func (s *server) handleRequest(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ctx = newContext(ctx, r)

	switch r.Method {
	case "GET":
		s.playbackStream(ctx, w, r)
	case "POST":
		s.recordStream(ctx, w, r)
	case "DELETE":
		s.deleteStream(ctx, w, r)
	}
}

func (s *server) recordStream(ctx context.Context, w http.ResponseWriter, r *http.Request) {
	name := r.URL.Path

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

	in := stream.StreamIn(name)
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

				go s.notifyUpstream(name, "closed")

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

func (s *server) deleteStream(ctx context.Context, w http.ResponseWriter, r *http.Request) {
	name := r.URL.Path

	if err := stream.StreamDelete(name); err != nil {
		s.handleError(w, r, err)
	} else {
		w.WriteHeader(204)
		w.(http.Flusher).Flush()
	}
}

func (s *server) playbackStream(ctx context.Context, w http.ResponseWriter, r *http.Request) {
	name := r.URL.Path

	out := stream.StreamOut(name)
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
	}

	switch ur.StatusCode {
	case 202:

		if rr, err := parseRewriteRequest(r, ur); err != nil {
			s.handleError(w, r, err)
		} else {
			next(w, rr)
		}
	case 204:
		next(w, r)
	default:
		if ur.Header.Get("X-Htee-Downstream-Continue") != "" {
			ur.Header.Del("X-Htee-Downstream-Continue")

			if rr, err := cloneRequest(r); err != nil {
				s.handleError(w, r, err)
			} else {
				go next(httptest.NewRecorder(), rr)
			}
		}

		proxyResponse(w, ur)
	}
}
