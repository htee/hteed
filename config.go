package htee

import (
	"flag"
	"fmt"
	"os"
)

var (
	callbacks = make([]func(*Config), 0)
)

type Config struct {
	Addr     string
	Port     int
	RedisUrl string
}

func Configure() *Config {
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

	for _, cb := range callbacks {
		cb(c)
	}

	return c
}

func ConfigCallback(cb func(*Config)) {
	callbacks = append(callbacks, cb)
}

func testConfigure() *Config {
	c := &Config{
		Addr:     "127.0.0.1",
		Port:     3000,
		RedisUrl: "127.0.0.1:6379",
	}

	for _, cb := range callbacks {
		cb(c)
	}

	return c
}
