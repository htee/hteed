package htee

import (
	"fmt"
	"net/http"
	"strconv"

	"github.com/gorilla/mux"
)

func playbackSSEStream(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	owner := vars["owner"]
	name := vars["name"]

	responseStarted := false
	out := StreamOut(owner, name)
	sseOut := formatSSEData(out.Out())
	cc := w.(http.CloseNotifier).CloseNotify()

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
			str := strconv.Quote(string(buf))
			ec <- "data:" + str[1:len(str)-1] + "\n"
		}

		close(ec)
	}()

	return ec
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
      if(resetLine == true) {
        cursor.innerHTML = data;
      } else {
        cursor.innerHTML += data;
      }

      resetLine = false;
    }

    source.onmessage = function(e) {
      append(e.data);
    };

    source.onerror = function(e) {
      console.log(e);
    };

    source.addEventListener('ctrl', function(e) {
      if(e.data == 'newline') {
        append("\n");
        cursor = $("<code></code>").insertAfter(cursor)[0];
      } else if(e.data == 'crlf' || e.data == 'carriage-return') {
        resetLine = true;
      } else if(e.data == 'eof') {
        source.close();
      } else {
        console.log(e);
      }
    }, false);

    window.setInterval(function(){
      window.scrollTo(0, document.body.scrollHeight);
    }, 100);

  </script>
</body>
</html>
`
