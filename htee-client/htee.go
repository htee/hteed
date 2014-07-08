package main

import (
	"bufio"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"os"
	"strings"

	"github.com/benburkert/htee"
)

var usage = strings.TrimSpace(`
htee

Usage:
    htee [<name>]
    htee [<name>] [--token=<token>]
    htee -h | --help

Options:
    -c, --config FILE       Configuration file
    -e, --endpoint URL      htee URL
    -l, --login NAME        Login name
    -t, --token TOKEN       API authentication token
    -n, --name NAME         Stream name
    -h, --help              Show help
`)

func main() {
	var configFile, streamName string
	var showHelp bool

	fs := flag.NewFlagSet("htee", flag.ContinueOnError)
	fs.SetOutput(ioutil.Discard)
	fs.StringVar(&configFile, "c", "", "")
	fs.StringVar(&configFile, "-config", "~/.htee", "")
	fs.BoolVar(&showHelp, "h", false, "")
	fs.BoolVar(&showHelp, "-help", false, "")
	fs.StringVar(&streamName, "n", "", "")
	fs.StringVar(&streamName, "-name", "", "")

	fs.Parse(os.Args[1:])

	if showHelp {
		fmt.Printf("%s\n", usage)
		os.Exit(0)
	}

	config := loadConfig(configFile)
	client, err := htee.NewClient(config)
	if err != nil {
		fmt.Fprintf(os.Stderr, err.Error())

		os.Exit(1)
	}

	buffOut := newBufferedWriter(io.Writer(os.Stdout))
	rc := ioutil.NopCloser(io.TeeReader(io.Reader(os.Stdin), buffOut))

	res, err := client.PostStream(streamName, rc)
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

func loadConfig(configFile string) *htee.ClientConfig {
	config := &htee.ClientConfig{
		Endpoint: "https://htee.io/",
	}

	gconfig := &htee.Config{
		ConfigFile: configFile,
		Client:     config,
	}

	if err := gconfig.Load(); err != nil {
		fmt.Printf("%s\n", usage)
		fmt.Printf("%s\n", err.Error())
		os.Exit(1)
	}

	fs := flag.NewFlagSet("htee", flag.ContinueOnError)

	fs.StringVar(&config.Endpoint, "e", config.Endpoint, "")
	fs.StringVar(&config.Endpoint, "-endpoint", config.Endpoint, "")

	fs.StringVar(&config.Token, "t", config.Token, "")
	fs.StringVar(&config.Token, "-token", config.Token, "")

	fs.StringVar(&config.Login, "l", config.Login, "")
	fs.StringVar(&config.Login, "-login", config.Login, "")

	if err := fs.Parse(os.Args[1:]); err != nil {
		fmt.Printf("%s\n", usage)
		fmt.Printf("%s\n", err.Error())
		os.Exit(1)
	}

	return config
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
