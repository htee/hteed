package main

import (
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"strings"

	"github.com/benburkert/htee"
	http "github.com/benburkert/httplus"
)

var usage = strings.TrimSpace(`
htee

Usage:
    htee
	htee [--anonymous]
    htee [--token=<token>]
    htee -h | --help

Options:
    -a, --anon, --anonymous Anonymous stream
    -c, --config FILE       Configuration file
    -e, --endpoint URL      htee URL
    -t, --token TOKEN       API authentication token
    -h, --help              Show help
`)

func main() {
	var configFile string
	var showHelp bool
	var arguments []string

	fs := flag.NewFlagSet("htee", flag.ContinueOnError)
	fs.SetOutput(ioutil.Discard)
	fs.StringVar(&configFile, "c", "~/.htee", "")
	fs.StringVar(&configFile, "config", configFile, "")
	fs.BoolVar(&showHelp, "h", false, "")
	fs.BoolVar(&showHelp, "help", showHelp, "")

	fs.Parse(arguments)

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

	res, err := client.PostStream(rc)
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
		fmt.Printf("%s\n\n", usage)
		fmt.Printf("%s\n", err.Error())
		os.Exit(1)
	}

	fs := flag.NewFlagSet("htee", flag.ContinueOnError)
	fs.SetOutput(ioutil.Discard)

	fs.StringVar(&config.Endpoint, "e", config.Endpoint, "")
	fs.StringVar(&config.Endpoint, "endpoint", config.Endpoint, "")

	fs.StringVar(&config.Token, "t", config.Token, "")
	fs.StringVar(&config.Token, "token", config.Token, "")

	fs.StringVar(&config.Login, "l", config.Login, "")
	fs.StringVar(&config.Login, "login", config.Login, "")

	fs.BoolVar(&config.Anonymous, "a", config.Anonymous, "")
	fs.BoolVar(&config.Anonymous, "anon", config.Anonymous, "")
	fs.BoolVar(&config.Anonymous, "anonymous", config.Anonymous, "")

	var __ string
	fs.StringVar(&__, "c", "", "")
	fs.StringVar(&__, "config", "", "")

	if err := fs.Parse(os.Args[1:]); err != nil {
		fmt.Printf("%s\n\n", usage)
		fmt.Printf("%s\n", err.Error())
		os.Exit(1)
	}

	return config
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
