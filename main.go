package main

import (
	"fmt"
	"net/http"

	"github.com/gorilla/mux"

	"github.com/htio/htsd/config"
)

func ChunkedHandler(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(204)
	fmt.Fprintf(w, "TransferEncoding: %s", r.TransferEncoding[0])
}

func main() {
	c := config.New()
	r := mux.NewRouter()

	r.Headers("Transfer-Encoding", "chunked")
	r.HandleFunc("/", chunkedHandler)

	http.ListenAndServe(c.Addr, r)
}
