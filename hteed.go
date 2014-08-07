package main

import (
	"flag"
	"fmt"
	"io/ioutil"
	"net/http"
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

	http.ListenAndServe(c.Addr(), server.ServerHandler())
}

func loadConfig(configFile string) *config.ServerConfig {
	cfg := &config.ServerConfig{
		Address:  "0.0.0.0",
		Port:     4000,
		RedisURL: ":6379",
		WebURL:   "http://0.0.0.0:3000/",
	}

	gconfig := &config.Config{
		ConfigFile: configFile,
		Server:     cfg,
	}

	if err := gconfig.Load(); err != nil {
		fmt.Printf("%s\n\n", usage)
		fmt.Printf("%s\n", err.Error())
		os.Exit(1)
	}

	fs := flag.NewFlagSet("hteed", flag.ContinueOnError)
	fs.SetOutput(ioutil.Discard)

	fs.StringVar(&cfg.Address, "a", cfg.Address, "")
	fs.StringVar(&cfg.Address, "address", cfg.Address, "")

	fs.IntVar(&cfg.Port, "p", cfg.Port, "")
	fs.IntVar(&cfg.Port, "port", cfg.Port, "")

	fs.StringVar(&cfg.RedisURL, "r", cfg.RedisURL, "")
	fs.StringVar(&cfg.RedisURL, "redis-url", cfg.RedisURL, "")

	fs.StringVar(&cfg.WebURL, "w", cfg.WebURL, "")
	fs.StringVar(&cfg.WebURL, "web-url", cfg.WebURL, "")

	var __ string
	fs.StringVar(&__, "c", "", "")
	fs.StringVar(&__, "cfg", "", "")

	if err := fs.Parse(os.Args[1:]); err != nil {
		fmt.Printf("%s\n\n", usage)
		fmt.Printf("%s\n", err.Error())
		os.Exit(1)
	}

	return cfg
}
