// +build integration

package htee

import (
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/benburkert/httplus"
	"github.com/garyburd/redigo/redis"
)

func init() {
	us := httptest.NewServer(http.HandlerFunc(fakeWebHandler))

	sc := &ServerConfig{
		Address:   "127.0.0.1",
		Port:      4000,
		RedisURL:  ":6379",
		WebURL:    us.URL,
		KeyPrefix: fmt.Sprintf("%d:", os.Getpid()),
	}

	if err := Configure(sc); err != nil {
		panic(err.Error())
	}
}

func testClient(url string) (*Client, error) {
	return NewClient(&ClientConfig{Endpoint: url, Login: "test"})
}

func delTestData() {
	conn := pool.Get()

	keys, err := redis.Strings(conn.Do("KEYS", keyPrefix+"*"))
	if err != nil {
		panic(err)
	}

	for _, key := range keys {
		conn.Do("DEL", key)
	}
}

func TestHelloWorldRoundTrip(t *testing.T) {
	defer delTestData()

	ts := httptest.NewServer(ServerHandler())
	defer ts.Close()

	client, err := testClient(ts.URL)
	if err != nil {
		t.Error(err)
	}

	pr, pw := io.Pipe()

	go func() {
		pw.Write([]byte("Hello, World!"))
		pw.Close()
	}()

	cntRes, err := client.PostStream(pr)
	if err != nil {
		t.Error(err)
	}

	owner, name := readNWO(cntRes)
	if owner != client.Login {
		t.Errorf("stream owner is %s, want %s", owner, client.Login)
	}

	res, err := client.GetStream(client.Login, name)
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
	defer delTestData()

	ts := httptest.NewServer(ServerHandler())
	defer ts.Close()

	client, err := testClient(ts.URL)
	if err != nil {
		t.Error(err)
	}

	pr, pw := io.Pipe()
	step, parts := make(chan interface{}), 100

	go func() {
		for i := 1; i <= parts; i++ {
			pw.Write([]byte(fmt.Sprintf("Part %d", i)))
			<-step
		}
		pw.Close()
	}()

	cntRes, err := client.PostStream(pr)
	if err != nil {
		t.Error(err)
	}

	owner, name := readNWO(cntRes)
	if owner != client.Login {
		t.Errorf("stream owner is %s, want %s", owner, client.Login)
	}

	res, err := client.GetStream(client.Login, name)
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

func TestStreamingFanOut(t *testing.T) {
	defer delTestData()

	ts := httptest.NewServer(ServerHandler())
	defer ts.Close()

	client, err := testClient(ts.URL)
	if err != nil {
		t.Error(err)
	}

	pr, pw := io.Pipe()
	step, parts, peers := make(chan interface{}), 1000, 100

	go func() {
		for i := 1; i <= parts; i++ {
			pw.Write([]byte(fmt.Sprintf("Part %d", i)))
			<-step
		}
		pw.Close()
	}()

	cntRes, err := client.PostStream(pr)
	if err != nil {
		t.Error(err)
	}

	owner, name := readNWO(cntRes)
	if owner != client.Login {
		t.Errorf("stream owner is %s, want %s", owner, client.Login)
	}

	responses := make([](*httplus.Response), peers)
	for i := range responses {
		if res, err := client.GetStream(client.Login, name); err != nil {
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

func TestEventStreamRequest(t *testing.T) {
	defer delTestData()

	ts := httptest.NewServer(ServerHandler())
	defer ts.Close()

	client, err := testClient(ts.URL)
	if err != nil {
		t.Error(err)
	}

	pr, pw := io.Pipe()
	chunks := []string{"Testing", " a ", "multi-chunk", " stream"}
	step := make(chan interface{})

	go func() {
		for _, chunk := range chunks {
			pw.Write([]byte(chunk))
			<-step
		}

		pw.Close()
	}()

	cntRes, err := client.PostStream(pr)
	if err != nil {
		t.Error(err)
	}

	owner, _ := readNWO(cntRes)
	if owner != client.Login {
		t.Errorf("stream owner is %s, want %s", owner, client.Login)
	}

	req, err := http.NewRequest("GET", ts.URL+cntRes.Header.Get("Location"), nil)
	if err != nil {
		t.Error(err)
	}
	req.Header.Add("Accept", "text/event-stream")

	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Error(err)
	}

	buf := make([]byte, 4096)
	for _, chunk := range chunks {
		if n, err := res.Body.Read(buf); err != nil {
			if err != io.EOF {
				t.Error(err)
			}
			break
		} else {
			data, err := json.Marshal(chunk)
			if err != nil {
				t.Error(err)
			}

			if dc := fmt.Sprintf("data:%s\n\n", data); string(buf[:n]) != dc {
				t.Errorf("response part is %q, want %q", buf[:n], dc)
			}
		}

		step <- true
	}
}

func TestDeleteStreamRequest(t *testing.T) {
	ts := httptest.NewServer(ServerHandler())

	client, err := testClient(ts.URL)
	if err != nil {
		t.Error(err)
	}

	body := ioutil.NopCloser(strings.NewReader("Goodbye World!"))

	cntRes, err := client.PostStream(body)
	if err != nil {
		t.Error(err)
	}

	owner, name := readNWO(cntRes)
	if owner != client.Login {
		t.Errorf("stream owner is %s, want %s", owner, client.Login)
	}

	res, err := client.GetStream(owner, name)
	res.Body.Close()
	if err != nil {
		t.Error(err)
	} else if res.StatusCode != 200 {
		t.Errorf("response was a %d status, want 200", res.StatusCode)
	}

	res, err = client.DeleteStream(owner, name)
	res.Body.Close()
	if err != nil {
		t.Error(err)
	} else if res.StatusCode != 204 {
		t.Errorf("response was a %d status, want 204", res.StatusCode)
	}

	res, err = client.GetStream(owner, name)
	if err != nil {
		t.Error(err)
	} else if res.StatusCode != 200 {
		t.Errorf("response was a %d status, want 200", res.StatusCode)
	}

	rb, err := ioutil.ReadAll(res.Body)
	res.Body.Close()
	if err != nil {
		t.Error(err)
	}

	if string(rb) != "" {
		t.Errorf("response body is '%s', want ''", rb)
	}
}

func fakeWebHandler(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/ping" {
		w.WriteHeader(200)
		w.Write([]byte("PONG!"))
		return
	}

	switch r.Method {
	case "GET", "DELETE":
		w.WriteHeader(204)
	case "POST":
		owner := "test"
		name := strconv.Itoa(time.Now().Nanosecond())

		message := struct{ Path string }{Path: owner + "/" + name}
		body, err := json.Marshal(message)
		if err != nil {
			panic(err.Error())
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(202)
		w.Write(body)
	}
}

func readNWO(res *httplus.Response) (owner, name string) {
	loc := res.Header.Get("Location")
	parts := strings.Split(loc[1:], "/")
	owner, name = parts[0], parts[1]
	return
}
