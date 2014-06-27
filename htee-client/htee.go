package main

import (
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"os"
	"os/user"

	"github.com/benburkert/htee"
	"github.com/nu7hatch/gouuid"
)

var endpoint = flag.String("endpoint", "http://127.0.0.1:3000", "URL of the ht service.")
var username = flag.String("username", currentuser(), "Username of the stream owner.")
var id = flag.String("id", uid(), "ID of the data stream.")
var help = flag.Bool("help", false, "Print help and exit")

func currentuser() string {
	if u, e := user.Current(); e != nil {
		panic(e.Error())
	} else {
		return u.Username
	}
}

func uid() string {
	uid, e := uuid.NewV4()

	if e != nil {
		panic(e.Error())
	}

	return uid.String()
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

	rc := ioutil.NopCloser(io.TeeReader(io.Reader(os.Stdin), io.Writer(os.Stdout)))

	client, err := htee.NewClient(*endpoint, *username)
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
