package main

import (
	"fmt"
	"net/http"

	"github.com/htio/htsd/config"
)

func handler(w http.ResponseWriter, r *http.Request) {
	fmt.Fprintf(w, "TransferEncoding: %s", r.TransferEncoding[0])
}

func main() {
	c := config.New()

	http.HandleFunc("/", handler)
	http.ListenAndServe(c.Addr, nil)
}
