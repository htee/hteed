// +build integration

package htee

import (
	"fmt"
	"io"
	"io/ioutil"
	"net/http/httptest"
	"testing"

	hteetp "github.com/benburkert/htee/http"
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
		t.Errorf("response body is '%s', want 'Hello, World!'", rb)
	}
}

func TestStreamingLockstep(t *testing.T) {
	ts := httptest.NewServer(ServerHandler())
	defer ts.Close()

	client := testClient(ts.URL)
	pr, pw := io.Pipe()
	step, parts := make(chan interface{}), 100

	go func() {
		if _, err := client.PostStream("lockstep", pr); err != nil {
			t.Error(err)
		}
	}()

	go func() {
		for i := 1; i <= parts; i++ {
			<-step
			pw.Write([]byte(fmt.Sprintf("Part %d", i)))
		}
		pw.Close()

		<-step
	}()

	step <- true

	res, err := client.GetStream(client.Username, "lockstep")
	if err != nil {
		t.Error(err)
	}

	buf := make([]byte, 4096)
	for i := 1; i <= parts; i++ {
		n, err := res.Body.Read(buf)
		if err != nil {
			if err != io.EOF {
				t.Error(err)
			}
		} else if string(buf[:n]) != fmt.Sprintf("Part %d", i) {
			t.Errorf("response part is '%s', want 'Part %d'", buf[:n], i)
		}

		step <- true
	}
}

func TestStreamingFanout(t *testing.T) {
	ts := httptest.NewServer(ServerHandler())
	defer ts.Close()

	client := testClient(ts.URL)
	pr, pw := io.Pipe()
	step, parts, peers := make(chan interface{}), 1000, 100

	go func() {
		if _, err := client.PostStream("fanout", pr); err != nil {
			t.Error(err)
		}
	}()

	go func() {
		for i := 1; i <= parts; i++ {
			<-step
			pw.Write([]byte(fmt.Sprintf("Part %d", i)))
		}
		pw.Close()

		<-step
	}()

	step <- true

	responses := make([](*hteetp.Response), peers)
	for i := range responses {
		if res, err := client.GetStream(client.Username, "fanout"); err != nil {
			t.Error(err)
		} else {
			responses[i] = res
		}
	}

	buf := make([]byte, 4096)
	for i := 1; i <= parts; i++ {
		for j, res := range responses {
			n, err := res.Body.Read(buf)
			if err != nil {
				if err != io.EOF {
					t.Error(err)
				}
			} else if string(buf[:n]) != fmt.Sprintf("Part %d", i) {
				t.Errorf("response %d part is '%s', want 'Part %d'", j, buf[:n], i)
			}
		}

		step <- true
	}
}
