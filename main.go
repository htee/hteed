package main

import (
	"fmt"
	"io"

	"github.com/benburkert/http"
	"github.com/htio/htsd/config"
)

func chunkedHandler(w http.ResponseWriter, r *http.Request) {
	buf := make([]byte, 4096)
	var n int
	var err error

	for {
		n, err = r.Body.Read(buf)

		if err != nil && err != io.EOF {
			fmt.Println(err.Error())
		}

		if n > 0 {
			fmt.Printf("%s", buf[:n])
		} else {
			goto respond
		}
	}

respond:
	w.WriteHeader(204)
	fmt.Fprintf(w, "TransferEncoding: %s", r.TransferEncoding[0])
}

func main() {
	c := config.New()
	http.HandleFunc("/", chunkedHandler)

	http.ListenAndServe(c.Addr, nil)
}
