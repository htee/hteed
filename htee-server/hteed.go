package main

import (
	"net/http"

	"github.com/benburkert/htee"
)

func main() {
	c := htee.Configure()

	http.ListenAndServe(c.Addr, htee.ServerHandler())
}
