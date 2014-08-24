package server

import (
	"bufio"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/htee/hteed/Godeps/_workspace/src/code.google.com/p/go.net/context"

	"github.com/htee/hteed/Godeps/_workspace/src/github.com/codegangsta/negroni"
	"github.com/htee/hteed/config"
	"github.com/htee/hteed/proxy"
	"github.com/htee/hteed/stream"
)

var Server *server

func init() {
	config.ConfigCallback(configureServer)
}

func configureServer(cnf *config.Config) error {
	Server = &server{
		logger: log.New(os.Stdout, "[server] ", log.LstdFlags),
	}

	return nil
}

type server struct {
	logger *log.Logger
}

func ListenAndServe(addr string) {
	Server.logger.Printf("Listening on %s", addr)
	defer Server.logger.Printf("Finished listening on %s", addr)

	http.ListenAndServe(addr, Server.ServerHandler())
}

func (s *server) ServerHandler() http.Handler {
	n := negroni.New()
	n.Use(negroni.HandlerFunc(s.upstreamMiddleware))
	n.Use(negroni.HandlerFunc(s.fixRailsVerbMiddleware))

	n.UseHandler(http.HandlerFunc(s.handleRequest))

	return n
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

func (s *server) recordStream(ctx context.Context, res http.ResponseWriter, req *http.Request) {
	name := req.URL.Path

	conn, hw, err := res.(http.Hijacker).Hijack()
	if err != nil {
		s.handleError(res, req, err)
		return
	}

	defer conn.Close()

	if err := writeContinue(hw, name); err != nil {
		s.handleError(res, req, err)
		return
	}

	reader := &io.LimitedReader{R: req.Body, N: 1 << 20}
	in := stream.In(ctx, name, reader)

	select {
	case <-in.Done():
		if in.Err != nil {
			s.handleError(res, req, in.Err)
		} else {
			if err := writeNoContent(hw); err != nil {
				s.handleError(res, req, err)
			}
		}
	case <-res.(http.CloseNotifier).CloseNotify():
		in.Cancel()
	}
}

func writeContinue(bw *bufio.ReadWriter, loc string) error {
	return writeResponse(bw,
		"HTTP/1.1 100 Continue\r\n",
		"Location: "+loc+"\r\n\r\n")
}

func writeNoContent(bw *bufio.ReadWriter) error {
	return writeResponse(bw,
		"HTTP/1.1 204 No Content\r\n",
		"Date: "+time.Now().UTC().Format(time.RFC1123)+"\r\n",
		"Connection: close\r\n\r\n")
}

func writeResponse(bw *bufio.ReadWriter, body ...string) error {
	for _, chunk := range body {
		if _, err := bw.WriteString(chunk); err != nil {
			return err
		}
	}

	return bw.Flush()
}

func (s *server) deleteStream(ctx context.Context, res http.ResponseWriter, req *http.Request) {
	name := req.URL.Path

	if err := stream.StreamDelete(ctx, name); err != nil {
		s.handleError(res, req, err)
	} else {
		res.WriteHeader(204)
		res.(http.Flusher).Flush()
	}
}

func (s *server) playbackStream(ctx context.Context, res http.ResponseWriter, req *http.Request) {
	name := req.URL.Path

	writer := res.(io.Writer)
	flusher := res.(http.Flusher)

	if isSSE(req) {
		res.Header().Set("Content-Type", "text/event-stream")
		writer = sseWriter{res}
	}

	res.WriteHeader(200)
	out := stream.Out(ctx, name, flushWriter{flusher, writer})

	select {
	case <-out.Done():
		if out.Err != nil {
			s.handleError(res, req, out.Err)
		}
	case <-res.(http.CloseNotifier).CloseNotify():
		out.Cancel()
	}
}

type flushWriter struct {
	f http.Flusher
	w io.Writer
}

func (w flushWriter) Write(p []byte) (int, error) {
	defer w.f.Flush()

	return w.w.Write(p)
}

func (s *server) upstreamMiddleware(w http.ResponseWriter, r *http.Request, next http.HandlerFunc) {
	pc := proxy.ProxyHTTP(r)

	// Hijack is incompatible with use of CloseNotifier
	var cn <-chan bool
	if !(r.Method == "POST" || r.Method == "PUT") {
		cn = w.(http.CloseNotifier).CloseNotify()
	} else {
		cn = make(chan bool)
	}

	select {
	case <-cn:
		pc.Cancel()
		return
	case <-pc.Done():
		if pc.Err != nil {
			s.handleError(w, r, pc.Err)
			return
		}
	}

	pr := pc.Response
	if pr == nil {
		return
	}

	if requestRewritten(pr) {
		if rr, err := proxy.RewriteRequest(r, pr); err != nil {
			s.handleError(w, r, err)
		} else {
			r = rr
		}
	}

	if nopContinue(pr) {
		go next(responseDiscarder(), proxy.CopyRequest(r))
	}

	if continueRequest(pr) {
		next(w, r)
	} else {
		proxy.Copy(w, pr)
	}
}

func (s *server) handleError(w http.ResponseWriter, r *http.Request, err error) {
	s.logger.Printf("%s - ERROR: %s", r.RemoteAddr, err.Error())
	http.Error(w, err.Error(), http.StatusInternalServerError)
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

func requestRewritten(res *http.Response) bool { return res.StatusCode == 202 }

func nopContinue(res *http.Response) bool {
	if res.Header.Get("X-Htee-Downstream-Continue") != "" {
		res.Header.Del("X-Htee-Downstream-Continue")

		return true
	}

	return false
}

func continueRequest(res *http.Response) bool {
	return requestRewritten(res) || res.StatusCode == 204
}

func responseDiscarder() http.ResponseWriter {
	return &discarder{ioutil.Discard, make(map[string][]string)}
}

type discarder struct {
	io.Writer

	hdr http.Header
}

func (d *discarder) WriteHeader(int) {}

func (d *discarder) Flush() {}

func (d *discarder) Header() http.Header { return d.hdr }
