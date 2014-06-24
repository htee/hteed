package htee

import (
	"io"
	"net/url"
	"strings"

	"github.com/benburkert/htee/http"
)

func testClient(endpoint string) *Client {
	url, err := url.ParseRequestURI(endpoint)
	if err != nil {
		panic(err.Error())
	}

	return &Client{
		c:        &http.Client{},
		endpoint: url,
		Username: "test",
	}
}

type Client struct {
	c *http.Client

	endpoint *url.URL
	Username string
}

func (c *Client) GetStream(owner, name string) (*http.Response, error) {
	url, err := c.endpoint.Parse(buildNWO(owner, name))
	if err != nil {
		return nil, err
	}

	return c.c.Do(buildGet(url))
}

func (c *Client) PostStream(name string, body io.ReadCloser) (*http.Response, error) {
	url, err := c.endpoint.Parse(buildNWO(c.Username, name))
	if err != nil {
		return nil, err
	}

	return c.c.Do(buildPost(url, body))
}

func buildGet(url *url.URL) *http.Request {
	return buildRequest("GET", url, nil)
}

func buildPost(url *url.URL, body io.ReadCloser) *http.Request {
	return buildRequest("POST", url, body)
}

func buildRequest(method string, url *url.URL, body io.ReadCloser) *http.Request {
	return &http.Request{
		Method:     method,
		Proto:      "HTTP/1.1",
		ProtoMajor: 1,
		ProtoMinor: 1,
		URL:        url,
		Body:       body,
		Header: http.Header{
			"Transfer-Encoding": {"chunked"},
			"Connection":        {"Keep-Alive"},
		},
		TransferEncoding: []string{"chunked"},
	}
}

func buildNWO(owner, name string) string {
	return strings.Join([]string{owner, name}, "/")
}
