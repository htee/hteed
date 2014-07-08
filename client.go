package htee

import (
	"io"
	"net/http"
	"net/url"
	"strings"
)

func NewClient(cnf *ClientConfig) (*Client, error) {
	url, err := url.ParseRequestURI(cnf.Endpoint)
	if err != nil {
		return nil, err
	}

	return &Client{
		c:        &http.Client{},
		Endpoint: url,
		Login:    cnf.Login,
		token:    cnf.Token,
	}, nil
}

type Client struct {
	c *http.Client

	Endpoint *url.URL
	Login    string
	token    string
}

func (c *Client) GetStream(owner, name string) (*http.Response, error) {
	url, err := c.Endpoint.Parse(buildNWO(owner, name))
	if err != nil {
		return nil, err
	}

	return c.c.Do(buildGet(url))
}

func (c *Client) PostStream(name string, body io.ReadCloser) (*http.Response, error) {
	url, err := c.Endpoint.Parse(buildNWO(c.Login, name))
	if err != nil {
		return nil, err
	}

	return c.c.Do(c.buildPost(url, body))
}

func buildGet(url *url.URL) *http.Request {
	return buildRequest("GET", url, defaultHeader(), nil)
}

func (c *Client) buildPost(url *url.URL, body io.ReadCloser) *http.Request {
	hdr := defaultHeader()
	hdr.Set("Expect", "100-Continue")
	hdr.Set("Authorization", "Token "+c.token)

	return buildRequest("POST", url, hdr, body)
}

func buildRequest(method string, url *url.URL, hdr http.Header, body io.ReadCloser) *http.Request {
	return &http.Request{
		Method:           method,
		Proto:            "HTTP/1.1",
		ProtoMajor:       1,
		ProtoMinor:       1,
		URL:              url,
		Body:             body,
		Header:           hdr,
		TransferEncoding: []string{"chunked"},
	}
}

func buildNWO(owner, name string) string {
	return strings.Join([]string{owner, name}, "/")
}

func defaultHeader() http.Header {
	return http.Header{
		"Transfer-Encoding": {"chunked"},
		"Connection":        {"Keep-Alive"},
	}
}
