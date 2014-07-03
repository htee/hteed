package main

import (
	"bufio"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"os"
	"os/user"

	"github.com/benburkert/htee"
)

var endpoint = flag.String("endpoint", "http://127.0.0.1:4000", "URL of the ht service.")
var username = flag.String("owner", currentuser(), "Login of the stream owner.")
var token = flag.String("token", "", "API token for authentication.")
var id = flag.String("name", "", "Name of the data stream.")
var help = flag.Bool("help", false, "Print help and exit")

func currentuser() string {
	if u, e := user.Current(); e != nil {
		panic(e.Error())
	} else {
		return u.Username
	}
}

func printHelp() {
	fmt.Println("ht - http + tee\n")

	flag.PrintDefaults()
}

func main() {
	flag.Parse()

	if *help {
		printHelp()
		os.Exit(0)
	}

	buffOut := newBufferedWriter(io.Writer(os.Stdout))
	rc := ioutil.NopCloser(io.TeeReader(io.Reader(os.Stdin), buffOut))

	client, err := htee.NewClient(*endpoint, *username, *token)
	if err != nil {
		fmt.Fprintf(os.Stderr, err.Error())

		os.Exit(1)
	}

	res, err := client.PostStream(*id, rc)
	if err != nil {
		fmt.Fprintf(os.Stderr, err.Error())

		os.Exit(1)
	}

	if res.StatusCode != http.StatusContinue {
		fmt.Fprintf(os.Stderr, "unexpected server response %q\n", res.Status)
		os.Exit(2)
	}

	if path := res.Header.Get("Location"); path == "" {
		os.Stderr.Write([]byte("server did not supply a Location header\n"))
		os.Exit(3)
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
		os.Exit(4)
	}
}

func newBufferedWriter(w io.Writer) *bufferedWriter {
	return &bufferedWriter{
		w: bufio.NewWriter(w),
		f: false,
	}
}

type bufferedWriter struct {
	w *bufio.Writer
	f bool
}

func (fw *bufferedWriter) Write(p []byte) (nn int, err error) {
	if nn, err = fw.w.Write(p); err != nil {
		return
	}

	if fw.f {
		err = fw.w.Flush()
	}

	return
}

func (fw *bufferedWriter) Flush() error {
	fw.f = true

	return fw.w.Flush()
}
