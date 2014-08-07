package config

import (
	"io/ioutil"
	"os"
	"reflect"
	"testing"
	"text/template"
)

func TestConfigLoad(t *testing.T) {
	tmpl, err := template.New("config").Parse(configTemplate)
	if err != nil {
		t.Error(err)
	}

	f, err := ioutil.TempFile(os.TempDir(), "hteed.conf")
	if err != nil {
		t.Error(err)
	}

	expected := &Config{
		Address:  "10.11.13.14",
		Port:     1234,
		RedisURL: "10.11.13.14:6379",
		WebToken: "deadbeef",
	}

	tmpl.Execute(f, expected)

	actual := new(Config)
	actual.Load(f.Name())

	if !reflect.DeepEqual(expected, actual) {
		t.Errorf("Parsed unexpected config:\nActual: %#v\nExpected: %#v", actual, expected)
	}
}

var configTemplate = `
address="{{.Address}}"
port={{.Port}}
web-token="{{.WebToken}}"
web-url="{{.WebURL}}"
redis-url="{{.RedisURL}}"
`
