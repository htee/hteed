package main

import (
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"net/url"
	"os"
	"strings"

	"github.com/nu7hatch/gouuid"
)

var endpoint = flag.String("endpoint", "http://127.0.0.1:3000", "URL of the ht service.")
var namespace = flag.String("namespace", hostname(), "Namespace of the data stream.")
var id = flag.String("id", uid(), "ID of the data stream.")
var contentType = flag.String("content-type", "text/plain", "Content type of the data stream.")
var help = flag.Bool("help", false, "Print help and exit")

func hostname() string {
	hn, e := os.Hostname()

	if e != nil {
		panic(e.Error())
	}

	return hn
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

func buildURL() *url.URL {
	url, err := url.ParseRequestURI(*endpoint)
	if err != nil {
		panic(err.Error())
	}

	path := strings.Join([]string{*namespace, *id}, "/")

	if url, err = url.Parse(path); err != nil {
		panic(err.Error())
	}

	return url
}

func buildRequest(url *url.URL) *http.Request {
	rc := ioutil.NopCloser(io.TeeReader(io.Reader(os.Stdin), io.Writer(os.Stdout)))

	return &http.Request{
		Method:     "POST",
		Proto:      "HTTP/1.1",
		ProtoMajor: 1,
		ProtoMinor: 1,
		URL:        url,
		Header: http.Header{
			"Content-Type":      {*contentType},
			"Transfer-Encoding": {"chunked"},
			"Connection":        {"Keep-Alive"},
		},
		TransferEncoding: []string{"chunked"},
		Body:             rc,
	}
}

func main() {
	flag.Parse()

	if *help {
		printHelp()
		os.Exit(0)
	}

	url := buildURL()

	fmt.Fprintf(os.Stderr, "%s\n", url)

	client := *http.DefaultClient
	req := buildRequest(url)

	res, err := client.Do(req)
	if err != nil {
		panic(err.Error())
	}

	if res.StatusCode != 204 {
		panic(fmt.Sprintf("unexpected server response %s\n", res.Status))
	}
}
