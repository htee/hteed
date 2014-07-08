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
	"testing"

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

	Configure(sc)
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

	if _, err := client.PostStream("helloworld", pr); err != nil {
		t.Error(err)
	}

	res, err := client.GetStream(client.Login, "helloworld")
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

	if _, err := client.PostStream("lockstep", pr); err != nil {
		t.Error(err)
	}

	res, err := client.GetStream(client.Login, "lockstep")
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

	if _, err := client.PostStream("fanout", pr); err != nil {
		t.Error(err)
	}

	responses := make([](*http.Response), peers)
	for i := range responses {
		if res, err := client.GetStream(client.Login, "fanout"); err != nil {
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

func TestStreamingFanIn(t *testing.T) {
	defer delTestData()

	ts := httptest.NewServer(ServerHandler())
	defer ts.Close()

	step, parts, peers := make(chan interface{}), 1000, 100

	clients := make([](*Client), peers)
	writers := make([](*io.PipeWriter), peers)
	readers := make([](*io.PipeReader), peers)

	for i := range writers {
		client, err := testClient(ts.URL)
		if err != nil {
			t.Error(err)
		}

		pr, pw := io.Pipe()

		clients[i] = client
		writers[i] = pw
		readers[i] = pr
	}

	go func() {
		for i := 1; i <= parts; i++ {
			for j := 0; j < peers; j++ {
				writers[j].Write([]byte(fmt.Sprintf("Peer %d Part %d", i, j)))
				<-step
			}
		}

		for i := 0; i < peers; i++ {
			writers[i].Close()
		}
	}()

	go func() {
		for i, client := range clients {
			if _, err := client.PostStream("fanin", readers[i]); err != nil {
				t.Error(err)
			}
		}
	}()

	client, err := testClient(ts.URL)
	if err != nil {
		t.Error(err)
	}

	res, err := client.GetStream(client.Login, "fanin")
	if err != nil {
		t.Error(err)
	}

	buf := make([]byte, 4096)
	for i := 1; i <= parts; i++ {
		for j := 0; j < peers; j++ {
			n, err := res.Body.Read(buf)
			if err != nil {
				if err != io.EOF {
					t.Error(err)
				}
			} else if string(buf[:n]) != fmt.Sprintf("Peer %d Part %d", i, j) {
				t.Errorf("response %d part is '%s', want 'Part %d'", j, buf[:n], i)
			}

			step <- true
		}
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

	name := "sse"
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

	cntRes, err := client.PostStream(name, pr)
	if err != nil {
		t.Error(err)
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

func fakeWebHandler(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(204)
}
