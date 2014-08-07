package server

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"strings"
)

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
