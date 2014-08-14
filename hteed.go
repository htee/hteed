package main

import (
	"flag"
	"fmt"
	"io/ioutil"
	"os"
	"strings"

	"github.com/htee/hteed/config"
	"github.com/htee/hteed/server"
)

var usage = strings.TrimSpace(`
hteed

Usage:
    hteed
    hteed -h | --help

Options:
    -c, --config FILE       Configuration file
    -a, --address HOST      Bind to host address
    -p, --port PORT         Bind to host port
    -r, --redis-url URL     Redis server connection string
    -w, --web-url URL       Upstream htee-web url
    -h, --help              Show help
`)

func main() {
	var configFile string
	var showHelp bool

	fs := flag.NewFlagSet("hteed", flag.ContinueOnError)
	fs.SetOutput(ioutil.Discard)
	fs.StringVar(&configFile, "c", "", "")
	fs.StringVar(&configFile, "config", "/etc/hteed/hteed.conf", "")
	fs.BoolVar(&showHelp, "h", false, "")
	fs.BoolVar(&showHelp, "help", false, "")

	fs.Parse(os.Args[1:])

	if showHelp {
		fmt.Printf("%s\n", usage)
		os.Exit(0)
	}
	c := loadConfig(configFile)

	if err := config.Configure(c); err != nil {
		fmt.Fprintf(os.Stderr, "%s\n", err.Error())
		os.Exit(1)
	}

	server.ListenAndServe(c.Addr())
}

func loadConfig(configFile string) *config.Config {
	cnf := &config.Config{
		Address:  "0.0.0.0",
		Port:     4000,
		RedisURL: ":6379",
		WebURL:   "http://0.0.0.0:3000/",
	}

	if err := cnf.Load(configFile); err != nil {
		fmt.Printf("%s\n\n", usage)
		fmt.Printf("%s\n", err.Error())
		os.Exit(1)
	}

	fs := flag.NewFlagSet("hteed", flag.ContinueOnError)
	fs.SetOutput(ioutil.Discard)

	fs.StringVar(&cnf.Address, "a", cnf.Address, "")
	fs.StringVar(&cnf.Address, "address", cnf.Address, "")

	fs.IntVar(&cnf.Port, "p", cnf.Port, "")
	fs.IntVar(&cnf.Port, "port", cnf.Port, "")

	fs.StringVar(&cnf.RedisURL, "r", cnf.RedisURL, "")
	fs.StringVar(&cnf.RedisURL, "redis-url", cnf.RedisURL, "")

	fs.StringVar(&cnf.WebURL, "w", cnf.WebURL, "")
	fs.StringVar(&cnf.WebURL, "web-url", cnf.WebURL, "")

	var __ string
	fs.StringVar(&__, "c", "", "")
	fs.StringVar(&__, "cnf", "", "")

	if err := fs.Parse(os.Args[1:]); err != nil {
		fmt.Printf("%s\n\n", usage)
		fmt.Printf("%s\n", err.Error())
		os.Exit(1)
	}

	return cnf
}
