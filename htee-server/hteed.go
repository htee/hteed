package main

import (
	"flag"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"strings"

	"github.com/benburkert/htee"
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
	fs.StringVar(&configFile, "-config", "/etc/hteed/hteed.conf", "")
	fs.BoolVar(&showHelp, "h", false, "")
	fs.BoolVar(&showHelp, "-help", false, "")

	fs.Parse(os.Args[1:])

	if showHelp {
		fmt.Printf("%s\n", usage)
		os.Exit(0)
	}
	c := loadConfig(configFile)

	if err := htee.Configure(c); err != nil {
		fmt.Fprintf(os.Stderr, "%s\n", err.Error())
		os.Exit(1)
	}

	http.ListenAndServe(c.Addr(), htee.ServerHandler())
}

func loadConfig(configFile string) *htee.ServerConfig {
	config := &htee.ServerConfig{
		Address:  "0.0.0.0",
		Port:     4000,
		RedisURL: ":6379",
		WebURL:   "http://0.0.0.0:3000/",
	}

	gconfig := &htee.Config{
		ConfigFile: configFile,
		Server:     config,
	}

	if err := gconfig.Load(); err != nil {
		fmt.Printf("%s\n", usage)
		fmt.Printf("%s\n", err.Error())
		os.Exit(1)
	}

	fs := flag.NewFlagSet("hteed", flag.ContinueOnError)
	fs.SetOutput(ioutil.Discard)

	fs.StringVar(&config.Address, "a", config.Address, "")
	fs.StringVar(&config.Address, "address", config.Address, "")

	fs.IntVar(&config.Port, "p", config.Port, "")
	fs.IntVar(&config.Port, "port", config.Port, "")

	fs.StringVar(&config.RedisURL, "r", config.RedisURL, "")
	fs.StringVar(&config.RedisURL, "redis-url", config.RedisURL, "")

	fs.StringVar(&config.WebURL, "w", config.WebURL, "")
	fs.StringVar(&config.WebURL, "web-url", config.WebURL, "")

	if err := fs.Parse(os.Args[1:]); err != nil {
		fmt.Printf("%s\n", usage)
		fmt.Printf("%s\n", err.Error())
		os.Exit(1)
	}

	return config
}
