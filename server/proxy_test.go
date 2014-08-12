package server

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/htee/hteed/config"
)

func TestPing(t *testing.T) {
	authToken := "deadbeef"

	pongHandler := func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/ping" {
			t.Errorf("expected /ping path, got %s", r.URL.Path)
		}

		authHeader := r.Header.Get("X-Htee-Authorization")
		if authHeader == "" {
			t.Errorf("missing X-Htee-Authorization header")
		} else if authHeader != "Token "+authToken {
			t.Errorf(`expected "Token %s" htee auth header, got "%s"`, authToken, authHeader)
		}

		w.WriteHeader(200)
		w.Write([]byte("PONG!"))
	}

	us := httptest.NewServer(http.HandlerFunc(pongHandler))

	cnf := &config.Config{
		WebToken: authToken,
		WebURL:   us.URL,
	}

	configureProxy(cnf)

	if err := Proxy.Ping(); err != nil {
		t.Error(err)
	}
}
