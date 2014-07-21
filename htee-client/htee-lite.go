package main

import (
	"fmt"
	"io"
	"io/ioutil"
	"os"

	"github.com/benburkert/htee"
	http "github.com/benburkert/httplus"
)

func main() {
	if len(os.Args) < 2 || os.Args[1] == "" {
		abort("missing url argument")
	}

	url := os.Args[1]

	client, err := htee.LiteClient(url)
	if err != nil {
		abort(err.Error())
	}

	rc := ioutil.NopCloser(io.TeeReader(io.Reader(os.Stdin), io.Writer(os.Stdout)))

	res, err := client.PostStream(rc)
	if err != nil {
		abort(err.Error())
	}

	if res.StatusCode != http.StatusContinue {
		abort("unexpected server response %q\n")
	}

	if path := res.Header.Get("Location"); path == "" {
		abort("server did not supply a Location header")
	} else {
		if _, err := client.Endpoint.Parse(path); err != nil {
			abort(err.Error())
		}
	}

	res, err = res.NextResponse()
	if err != nil {
		abort(err.Error())
	}

	if res.StatusCode != http.StatusNoContent {
		abort("unexpected server response %q\n")
	}
}

func abort(err string) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
