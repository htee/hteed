package htee

import (
	"io"
	"net/url"
	"strings"

	http "github.com/benburkert/httplus"
)

func NewClient(cnf *ClientConfig) (*Client, error) {
	url, err := url.ParseRequestURI(cnf.Endpoint)
	if err != nil {
		return nil, err
	}

	return &Client{
		c:         &http.Client{},
		Endpoint:  url,
		Login:     cnf.Login,
		token:     cnf.Token,
		anonymous: cnf.Anonymous,
	}, nil
}

type Client struct {
	c *http.Client

	Endpoint  *url.URL
	Login     string
	token     string
	anonymous bool
}

func (c *Client) GetStream(owner, name string) (*http.Response, error) {
	url, err := c.Endpoint.Parse(buildNWO(owner, name))
	if err != nil {
		return nil, err
	}

	return c.c.Do(buildGet(url))
}

func (c *Client) DeleteStream(owner, name string) (*http.Response, error) {
	url, err := c.Endpoint.Parse(buildNWO(owner, name))
	if err != nil {
		return nil, err
	}

	return c.c.Do(buildDelete(url))
}

func (c *Client) PostStream(body io.ReadCloser) (*http.Response, error) {
	return c.c.Do(c.buildPost(c.Endpoint, body))
}

func buildGet(url *url.URL) *http.Request {
	return buildRequest("GET", url, defaultHeader(), nil)
}

func buildDelete(url *url.URL) *http.Request {
	return buildRequest("DELETE", url, defaultHeader(), nil)
}

func (c *Client) buildPost(url *url.URL, body io.ReadCloser) *http.Request {
	hdr := defaultHeader()
	hdr.Set("Expect", "100-Continue")

	if !c.anonymous {
		hdr.Set("Authorization", "Token "+c.token)
	}

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
