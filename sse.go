package htee

import (
	"fmt"
	"net/http"
	"strconv"

	"github.com/gorilla/mux"
)

const MaxDataSize = 4096 // 4KB

func playbackSSEStream(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	owner := vars["owner"]
	name := vars["name"]

	responseStarted := false
	out := StreamOut(owner, name)
	sseOut := formatSSEData(out.Out())
	cc := w.(http.CloseNotifier).CloseNotify()

	hdr := w.Header()
	hdr.Set("Content-Type", "text/event-stream")
	hdr.Set("Cache-Control", "no-cache")
	hdr.Set("Connection", "close")

	defer out.Close()

	for {
		select {
		case err := <-out.Errors():
			fmt.Println(err.Error())

			if !responseStarted {
				w.WriteHeader(500)
			}

			return
		case <-cc:
			return
		case data, ok := <-sseOut:
			if ok {

				if !responseStarted {
					w.WriteHeader(200)
					responseStarted = true
				}

				w.Write([]byte(data))
				w.(http.Flusher).Flush()
			} else {
				w.Write([]byte("event:eof\n"))
				w.(http.Flusher).Flush()
				return
			}
		}
	}
}

func sendSSEShell(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(200)
	w.Write([]byte(SSEShell))
}

func formatSSEData(uc <-chan []byte) <-chan string {
	ec := make(chan string)

	go func() {
		for buf := range uc {
			for i, n := 0, min(MaxDataSize, len(buf)); i < len(buf); i += MaxDataSize {
				ec <- "data:" + strconv.Quote(string(buf[i:n])) + "\n\n"

				n = min(n+MaxDataSize, len(buf))
			}
		}

		close(ec)
	}()

	return ec
}

func min(a, b int) int {
	if a < b {
		return a
	} else {
		return b
	}
}

const SSEShell = `
<!DOCTYPE html>
<html>
<head>
  <meta charset="utf-8" />
  <script type="text/javascript" src="https://ajax.googleapis.com/ajax/libs/jquery/1.10.1/jquery.min.js"></script>
</head>
<body><pre><code></code></pre>
  <script>
    var source = new EventSource(window.location.pathname);
    var body = $("html,body");
    var cursor = $("pre code")[0];
    var resetLine = false;

    function append(data) {
      cursor.innerHTML += decodeURIComponent(data);
    }

    source.onmessage = function(e) {
      append(e.data);
    };

    source.onerror = function(e) {
      console.log(e);
    };

    source.addEventListener('eof', function(e) {
      source.close();
    }, false);

    window.setInterval(function(){
      window.scrollTo(0, document.body.scrollHeight);
    }, 100);

  </script>
</body>
</html>
`
