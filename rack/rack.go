package rack

import (
	"github.com/benburkert/http"
)

type MiddlewareFunc func(http.HandlerFunc, http.ResponseWriter, *http.Request)

type Middleware interface {
	Handler(http.HandlerFunc) http.HandlerFunc
}

type middleware struct {
	mf MiddlewareFunc
}

type Stack []Middleware

func ListenAndServe(addr string, stack Stack) error {
	return http.ListenAndServe(addr, stack.rackup())
}

func Build(mf MiddlewareFunc) Middleware {
	return middleware{mf}
}

func (m middleware) Handler(hf http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		m.mf(hf, w, r)
	}
}

type IfFunc func(*http.Request) bool

type iff struct {
	f IfFunc
	h http.HandlerFunc
}

func If(f IfFunc, s Stack) Middleware {
	return iff{f, s.rackup()}
}

func (i iff) Handler(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if i.f(r) {
			i.h(w, r)
		} else {
			h(w, r)
		}
	}
}

func Not(f IfFunc) IfFunc {
	return func(r *http.Request) bool {
		return !f(r)
	}
}

func (s Stack) rackup() http.HandlerFunc {
	handler := http.NotFound

	for i := len(s[1:]); i >= 0; i-- {
		handler = s[i].Handler(handler)
	}

	return handler
}

func IsChunkedPost(r *http.Request) bool {
	return r.Method == "POST" &&
		r.TransferEncoding[0] == "chunked"
}

func IsGet(r *http.Request) bool {
	return r.Method == "GET"
}
