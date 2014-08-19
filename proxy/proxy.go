package proxy

import (
	"bytes"
	"errors"
	"fmt"
	"time"

	"log"
	"net"
	"net/http"
	"net/url"
	"os"

	"github.com/htee/hteed/config"
)

func ProxyHTTP(req *http.Request) *Context {
	return Proxy.proxyHTTP(req)
}

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
		upstream:  upstream,
		authToken: cnf.WebToken,
		transport: &http.Transport{
			Dial: (&net.Dialer{
				Timeout:   30 * time.Second,
				KeepAlive: 30 * time.Second,
			}).Dial,
			TLSHandshakeTimeout: 10 * time.Second,
		},
	}

	if err := Proxy.Ping(); err != nil {
		return err
	}

	return nil
}

func (p *proxy) Ping() error {
	url, err := p.upstream.Parse("/ping")
	if err != nil {
		return err
	}

	rdr := bytes.NewReader([]byte{})
	req, err := http.NewRequest("GET", url.String(), rdr)
	if err != nil {
		return err
	}

	ctx := p.proxyHTTP(req)
	timeout := time.After(10 * time.Second)

	select {
	case <-ctx.Done():
		if ctx.Err != nil {
			return ctx.Err
		}
	case <-timeout:
		return errors.New("No pong reply before 5 second timeout")
	}

	if ctx.Response.StatusCode != 200 {
		return fmt.Errorf("Expected 200 OK response from upstream ping, got %s", ctx.Response.Status)
	}

	return nil
}

type proxy struct {
	logger    *log.Logger
	transport *http.Transport
	upstream  *url.URL
	authToken string
}

func (p *proxy) proxyHTTP(req *http.Request) *Context {
	preq := p.rewriteRequest(req)

	return ProxyContext(p, preq)
}

func (p *proxy) rewriteRequest(req *http.Request) *http.Request {
	rreq := CopyRequest(req)

	rreq.Host = p.upstream.Host
	rreq.URL.Scheme = p.upstream.Scheme
	rreq.URL.Host = p.upstream.Host
	rreq.URL.Path = singleJoiningSlash(p.upstream.Path, req.URL.Path)
	if p.upstream.RawQuery == "" || req.URL.RawQuery == "" {
		rreq.URL.RawQuery = p.upstream.RawQuery + req.URL.RawQuery
	} else {
		rreq.URL.RawQuery = p.upstream.RawQuery + "&" + req.URL.RawQuery
	}

	rreq.Header.Set("X-Htee-Authorization", p.authHeader())

	return rreq
}

func (p *proxy) authHeader() string { return "Token " + p.authToken }
