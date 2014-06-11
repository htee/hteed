package main

import (
	"flag"
	"fmt"
	"net/http"
	"os"
)

var addr = flag.String("address", "0.0.0.0", "Bind to host address (default is 0.0.0.0)")
var port = flag.String("port", "3000", "Bind to host port (default is 3000)")
var help = flag.Bool("help", false, "Print help and exit")

func handler(w http.ResponseWriter, r *http.Request) {
	fmt.Fprintf(w, "TransferEncoding: %s", r.TransferEncoding[0])
}

func main() {
	flag.Parse()

	if *help {
		fmt.Println("htsd - ht.io server")
		flag.PrintDefaults()

		os.Exit(0)
	}

	addrPort := *addr + ":" + *port

	http.HandleFunc("/", handler)
	http.ListenAndServe(addrPort, nil)
}
