package server

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"

	"github.com/htee/hteed/config"
)

var Proxy *proxy

func init() {
	config.ConfigCallback(configureProxy)
}

func configureProxy(cnf *config.Config) error {
	upstream, err := url.Parse(cnf.WebURL)
	if err != nil {
		return err
	}

	Proxy = &proxy{
		logger:    log.New(os.Stdout, "[proxy] ", log.LstdFlags),
		transport: http.DefaultTransport,
		upstream:  upstream,
		authToken: cnf.WebToken,
	}

	if err := Proxy.Ping(); err != nil {
		return err
	}

	return nil
}

type proxy struct {
	logger    *log.Logger
	transport http.RoundTripper
	upstream  *url.URL
	authToken string
}

func (p *proxy) ProxyHTTP(req *http.Request) *http.Response {
	preq := copyRequest(req)

	preq.URL.Scheme = p.upstream.Scheme
	preq.URL.Host = p.upstream.Host
	preq.URL.Path = singleJoiningSlash(p.upstream.Path, req.URL.Path)
	if p.upstream.RawQuery == "" || req.URL.RawQuery == "" {
		preq.URL.RawQuery = p.upstream.RawQuery + req.URL.RawQuery
	} else {
		preq.URL.RawQuery = p.upstream.RawQuery + "&" + req.URL.RawQuery
	}

	res, err := p.roundTrip(preq)
	if err != nil {
		p.logger.Printf("error: %v", err)
		return nil
	}

	return res
}

func (p *proxy) CopyResponse(dst http.ResponseWriter, src *http.Response) {
	defer src.Body.Close()

	for _, h := range hopHeaders {
		src.Header.Del(h)
	}

	copyHeader(dst.Header(), src.Header)

	dst.WriteHeader(src.StatusCode)
	io.Copy(dst, src.Body)
}

func (p *proxy) Ping() error {
	url, err := p.upstream.Parse("/ping")
	if err != nil {
		return err
	}

	req, err := http.NewRequest("GET", url.String(), nil)
	if err != nil {
		return err
	}

	res, err := p.roundTrip(req)
	if err != nil {
		return err
	}

	if res.StatusCode != 200 {
		return fmt.Errorf("Expected 200 OK response from upstream ping, got %s", res.Status)
	}

	return nil
}

func copyRequest(req *http.Request) *http.Request {
	outreq := new(http.Request)
	*outreq = *req // includes shallow copies of maps, but okay

	outreq.Proto = "HTTP/1.1"
	outreq.ProtoMajor = 1
	outreq.ProtoMinor = 1
	outreq.Close = false

	// Remove hop-by-hop headers to the backend.  Especially
	// important is "Connection" because we want a persistent
	// connection, regardless of what the client sent to us.  This
	// is modifying the same underlying map from req (shallow
	// copied above) so we only copy it if necessary.
	copiedHeaders := false
	for _, h := range hopHeaders {
		if outreq.Header.Get(h) != "" {
			if !copiedHeaders {
				outreq.Header = make(http.Header)
				copyHeader(outreq.Header, req.Header)
				copiedHeaders = true
			}
			outreq.Header.Del(h)
		}
	}

	if clientIP, _, err := net.SplitHostPort(req.RemoteAddr); err == nil {
		// If we aren't the first proxy retain prior
		// X-Forwarded-For information as a comma+space
		// separated list and fold multiple headers into one.
		if prior, ok := outreq.Header["X-Forwarded-For"]; ok {
			clientIP = strings.Join(prior, ", ") + ", " + clientIP
		}
		outreq.Header.Set("X-Forwarded-For", clientIP)
	}

	// If the request body is not chunked, tee it into the
	// proxy request's body & and replace it with a buffered
	// copy of the request body (it will be fully read by
	// the proxy request).
	if !isChunked(req) {
		bodyBuffer := bytes.NewBuffer(make([]byte, 0, req.ContentLength))
		bodyReader := io.TeeReader(req.Body, bodyBuffer)

		outreq.Body = ioutil.NopCloser(bodyReader)
		req.Body = ioutil.NopCloser(bodyBuffer)
	}

	return outreq
}

func (p *proxy) roundTrip(req *http.Request) (*http.Response, error) {
	req.Header.Set("X-Htee-Authorization", p.authHeader())

	return p.transport.RoundTrip(req)
}

func (p *proxy) authHeader() string {
	return "Token " + p.authToken
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

func proxyResponse(w http.ResponseWriter, r *http.Response) error {
	wh := w.Header()

	for k, v := range r.Header {
		wh[k] = v
	}

	w.WriteHeader(r.StatusCode)

	_, err := io.Copy(w, r.Body)
	return err
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

// copied from http/httputil/reverseproxy.go

// Hop-by-hop headers. These are removed when sent to the backend.
// http://www.w3.org/Protocols/rfc2616/rfc2616-sec13.html
var hopHeaders = []string{
	"Connection",
	"Keep-Alive",
	"Proxy-Authenticate",
	"Proxy-Authorization",
	"Te", // canonicalized version of "TE"
	"Trailers",
	"Transfer-Encoding",
	"Upgrade",
}

func copyHeader(dst, src http.Header) {
	for k, vv := range src {
		for _, v := range vv {
			dst.Add(k, v)
		}
	}
}

func singleJoiningSlash(a, b string) string {
	aslash := strings.HasSuffix(a, "/")
	bslash := strings.HasPrefix(b, "/")
	switch {
	case aslash && bslash:
		return a + b[1:]
	case !aslash && !bslash:
		return a + "/" + b
	}
	return a + b
}
