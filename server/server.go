package server

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/codegangsta/negroni"
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

func (s *server) deleteStream(w http.ResponseWriter, r *http.Request) {
	name := r.URL.Path

	if err := stream.StreamDelete(name); err != nil {
		s.handleError(w, r, err)
	} else {
		w.WriteHeader(204)
		w.(http.Flusher).Flush()
	}
}

func (s *server) playbackStream(w http.ResponseWriter, r *http.Request) {
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

func parseRewriteRequest(req *http.Request, res *http.Response) (*http.Request, error) {
	var message struct {
		Method, Path, Body string
		Headers            map[string]string
	}

	dec := json.NewDecoder(res.Body)
	if err := dec.Decode(&message); err != nil {
		return nil, err
	}

	r, err := cloneRequest(req)
	if err != nil {
		return nil, err
	}

	if message.Method != "" {
		r.Method = message.Method
	}

	if message.Path != "" {
		if url, err := req.URL.Parse(message.Path); err != nil {
			return nil, err
		} else {
			r.URL = url
		}
	}

	if len(message.Headers) > 0 {
		for k, v := range message.Headers {
			r.Header.Set(k, v)
		}
	}

	if message.Body != "" {
		r.Body = ioutil.NopCloser(strings.NewReader(message.Body))
	}

	return r, nil
}

func (s *server) proxyUpstream(r *http.Request) (*http.Response, error) {
	var body io.ReadCloser
	uu, err := s.upstream.Parse(r.URL.RequestURI())
	if err != nil {
		return nil, err
	}

	if !isChunked(r) {
		data, err := ioutil.ReadAll(r.Body)
		if err != nil {
			return nil, err
		}

		r.Body = ioutil.NopCloser(bytes.NewBuffer(data))
		body = ioutil.NopCloser(bytes.NewBuffer(data))
	}

	ur, err := http.NewRequest(r.Method, uu.String(), body)
	if err != nil {
		return nil, err
	}

	uh := ur.Header
	for k, _ := range r.Header {
		uh.Set(k, r.Header.Get(k))
	}

	uh.Set("X-Forwarded-Host", r.Host)
	uh.Set("X-Htee-Authorization", authHeader)

	return s.transport.RoundTrip(ur)
}

func (s *server) notifyUpstream(name, status string) {
	path, err := s.upstream.Parse(name)
	if err != nil {
		s.logger.Printf("notifyUpstream - ERROR: %s", err.Error())
		return
	}

	body, err := json.Marshal(map[string]string{"stream": name, "status": status})
	if err != nil {
		s.logger.Printf("notifyUpstream - ERROR: %s", err.Error())
		return
	}

	req, err := http.NewRequest("PUT", path.String(), bytes.NewReader(body))
	if err != nil {
		s.logger.Printf("notifyUpstream - ERROR: %s", err.Error())
		return
	}

	h := req.Header
	h.Set("X-Htee-Authorization", authHeader)
	h.Set("Content-Type", "application/json")

	res, err := s.transport.RoundTrip(req)
	if err != nil {
		s.logger.Printf("notifyUpstream - ERROR: %s", err.Error())
		return
	}

	if res.StatusCode != 204 {
		body, _ := ioutil.ReadAll(res.Body)
		msg := fmt.Sprintf("%s %s", res.Status, body)
		s.logger.Printf("notifyUpstream - Unexpected Response: %s", msg)
	}
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

func (s *server) fixRailsVerbMiddleware(w http.ResponseWriter, r *http.Request, next http.HandlerFunc) {
	if r.Method == "POST" && r.Header.Get("Content-Type") == "application/x-www-form-urlencoded" {
		r.ParseForm()

		if method := r.Form["_method"][0]; method != "" {
			r.Method = strings.ToUpper(method)
		}
	}

	next(w, r)
}

func isChunked(r *http.Request) bool {
	return len(r.TransferEncoding) > 0 && r.TransferEncoding[0] == "chunked"
}

func cloneRequest(r *http.Request) (*http.Request, error) {
	cr := new(http.Request)
	*cr = *r

	if !isChunked(r) {
		data, err := ioutil.ReadAll(r.Body)
		if err != nil {
			return nil, err
		}

		cr.Body = ioutil.NopCloser(bytes.NewBuffer(data))
	}

	return cr, nil
}
