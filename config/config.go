package config

import (
	"fmt"
	"os"
	"os/user"
	"reflect"
	"strconv"
	"strings"

	"github.com/BurntSushi/toml"
)

var (
	callbacks = make([]func(*Config) error, 0)
)

func ConfigCallback(cb func(*Config) error) {
	callbacks = append(callbacks, cb)
}

func Configure(c *Config) error {
	for _, cb := range callbacks {
		if err := cb(c); err != nil {
			return err
		}
	}

	return nil
}

type Config struct {
	KeyPrefix string
	Testing   bool

	Address  string `toml:"address" env:"HTEE_ADDRESS"`
	Port     int    `toml:"port" env"HTEE_PORT"`
	RedisURL string `toml:"redis-url" env:"REDIS_URL"`
	WebURL   string `toml:"web-url" env:"HTEE_WEB_URL"`
	WebToken string `toml:"web-token" env:"HTEE_WEB_TOKEN"`
}

func (c *Config) Addr() string {
	return c.Address + ":" + strconv.Itoa(c.Port)
}

func (c *Config) Load(cnfFile string) error {
	if err := c.loadConfigFile(cnfFile); err != nil {
		return err
	}

	if err := c.loadEnv(); err != nil {
		return err
	}

	return nil
}

func (c *Config) loadConfigFile(cnfFile string) error {
	if cnfFile[:2] == "~/" {

		usr, err := user.Current()
		if err != nil {
			return err
		}

		cnfFile = strings.Replace(cnfFile, "~", usr.HomeDir, 1)
	}

	if _, err := os.Stat(cnfFile); os.IsNotExist(err) {
		return nil
	}

	_, err := toml.DecodeFile(cnfFile, &c)
	return err
}

func (c *Config) loadEnv() error {
	value := reflect.Indirect(reflect.ValueOf(c))
	typ := value.Type()
	for i := 0; i < typ.NumField(); i++ {
		field := typ.Field(i)

		// Retrieve environment variable.
		v := strings.TrimSpace(os.Getenv(field.Tag.Get("env")))
		if v == "" {
			continue
		}

		// Set the appropriate type.
		switch field.Type.Kind() {
		case reflect.Bool:
			value.Field(i).SetBool(v != "0" && v != "false")
		case reflect.Int:
			newValue, err := strconv.ParseInt(v, 10, 0)
			if err != nil {
				return fmt.Errorf("Parse error: %s: %s", field.Tag.Get("env"), err)
			}
			value.Field(i).SetInt(newValue)
		case reflect.String:
			value.Field(i).SetString(v)
		case reflect.Float64:
			newValue, err := strconv.ParseFloat(v, 64)
			if err != nil {
				return fmt.Errorf("Parse error: %s: %s", field.Tag.Get("env"), err)
			}
			value.Field(i).SetFloat(newValue)
		}
	}
	return nil
}
