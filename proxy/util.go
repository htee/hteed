package proxy

import (
	"bytes"
	"encoding/json"
	"io"
	"io/ioutil"
	"net"
	"net/http"
	"strings"
)

func Copy(w http.ResponseWriter, r *http.Response) error {
	wh := w.Header()

	for k, v := range r.Header {
		wh[k] = v
	}

	w.WriteHeader(r.StatusCode)

	_, err := io.Copy(w, r.Body)
	return err
}

func RewriteRequest(req *http.Request, res *http.Response) (*http.Request, error) {
	var message struct {
		Method, Path, Body string
		Headers            map[string]string
	}

	dec := json.NewDecoder(res.Body)
	if err := dec.Decode(&message); err != nil {
		return nil, err
	}

	r := CopyRequest(req)

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
	} else {
		r.Body = req.Body
	}

	return r, nil
}

func isChunked(r *http.Request) bool {
	return len(r.TransferEncoding) > 0 && r.TransferEncoding[0] == "chunked"
}

// copied from http/httputil/reverseproxy.go

func CopyRequest(req *http.Request) *http.Request {
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
	} else {
		outreq.Body = ioutil.NopCloser(bytes.NewReader([]byte{}))
	}

	return outreq
}

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
