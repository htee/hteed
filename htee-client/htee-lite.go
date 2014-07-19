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
		fmt.Fprintf(os.Stderr, "missing url argument\n")

		os.Exit(1)
	}

	url := os.Args[1]

	client, err := htee.LiteClient(url)
	if err != nil {
		fmt.Fprintf(os.Stderr, err.Error())

		os.Exit(1)
	}

	buffOut := newBufferedWriter(io.Writer(os.Stdout))
	rc := ioutil.NopCloser(io.TeeReader(io.Reader(os.Stdin), buffOut))

	res, err := client.PostStream(rc)
	if err != nil {
		fmt.Fprintf(os.Stderr, err.Error())

		os.Exit(1)
	}

	if res.StatusCode != http.StatusContinue {
		fmt.Fprintf(os.Stderr, "unexpected server response %q\n", res.Status)
		os.Exit(1)
	}

	if path := res.Header.Get("Location"); path == "" {
		os.Stderr.Write([]byte("server did not supply a Location header\n"))
		os.Exit(1)
	} else {
		if u, err := client.Endpoint.Parse(path); err != nil {
			fmt.Fprintf(os.Stderr, err.Error())

			os.Exit(1)
		} else {
			fmt.Fprintf(os.Stderr, "%s\n", u.String())
		}
	}

	buffOut.Flush()

	res, err = res.NextResponse()
	if err != nil {
		fmt.Fprintf(os.Stderr, err.Error())

		os.Exit(1)
	}

	if res.StatusCode != http.StatusNoContent {
		fmt.Fprintf(os.Stderr, "unexpected server response %q\n", res.Status)
		os.Exit(1)
	}
}

func newBufferedWriter(w io.Writer) *bufferedWriter {
	return &bufferedWriter{
		writer: w,
		flush:  false,
		buffer: [][]byte{},
	}
}

type bufferedWriter struct {
	writer io.Writer
	flush  bool
	buffer [][]byte
}

func (fw *bufferedWriter) Write(p []byte) (nn int, err error) {
	var n int

	if fw.flush {
		for n, nn = 0, 0; nn < len(p); nn += n {
			if n, err = fw.writer.Write(p); err != nil {
				return
			}
		}
	} else {
		fw.buffer = append(fw.buffer, p)
	}

	return
}

func (fw *bufferedWriter) Flush() (err error) {
	if fw.flush {
		return
	}

	fw.flush = true

	for _, p := range fw.buffer {
		for n, nn := 0, 0; nn < len(p); nn += n {
			if n, err = fw.writer.Write(p); err != nil {
				return
			}
		}
	}

	return
}
