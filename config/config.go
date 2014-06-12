package config

import (
	"flag"
	"fmt"
	"os"
)

type Config struct {
	Addr     string
	Port     int
	RedisUrl string
}

func New() *Config {
	c := new(Config)

	addr := flag.String("address", "0.0.0.0", "Bind to host address (default is 0.0.0.0)")
	port := flag.String("port", "3000", "Bind to host port (default is 3000)")

	flag.StringVar(&c.RedisUrl, "redis-url", "", "Redis server URL (default is REDIS_URL)")

	help := flag.Bool("help", false, "Print help and exit")

	flag.Parse()

	if *help {
		fmt.Println("htsd - ht.io server")
		flag.PrintDefaults()

		os.Exit(0)
	}

	c.Addr = *addr + ":" + *port

	if c.RedisUrl == "" {
		c.RedisUrl = os.Getenv("REDIS_URL")
	}

	if c.RedisUrl == "" {
		c.RedisUrl = ":6379"
	}

	return c
}
