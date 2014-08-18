package proxy

import "net/http"

func ProxyContext(prx *proxy, req *http.Request) *Context {
	ctx := &Context{
		request:   req,
		transport: prx.transport,
		done:      make(chan struct{}),
	}

	go ctx.roundTrip()

	return ctx
}

type Context struct {
	Err      error
	Response *http.Response

	request   *http.Request
	transport *http.Transport
	done      chan struct{}
	closed    bool
}

func (c *Context) Cancel() {
	c.transport.CancelRequest(c.request)
	c.close()
}

func (pc *Context) Done() <-chan struct{} { return pc.done }

func (c *Context) roundTrip() {
	c.Response, c.Err = c.transport.RoundTrip(c.request)
	c.close()
}

func (c *Context) close() {
	if !c.closed {
		c.closed = true
		close(c.done)
	}
}
