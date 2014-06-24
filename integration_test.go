// +build integration

package htee

import (
	"io"
	"io/ioutil"
	"net/http/httptest"
	"testing"
)

func init() {
	testConfigure()
}

func TestHelloWorldRoundTrip(t *testing.T) {
	ts := httptest.NewServer(ServerHandler())
	defer ts.Close()

	client := testClient(ts.URL)
	pr, pw := io.Pipe()

	go func() {
		pw.Write([]byte("Hello, World!"))
		pw.Close()
	}()

	if _, err := client.PostStream("helloworld", pr); err != nil {
		t.Error(err)
	}

	res, err := client.GetStream(client.Username, "helloworld")
	if err != nil {
		t.Error(err)
	}

	rb, err := ioutil.ReadAll(res.Body)
	res.Body.Close()
	if err != nil {
		t.Error(err)
	}

	if string(rb) != "Hello, World!" {
		t.Errorf("Response body is '%s', want 'Hello, World!'", rb)
	}
}
