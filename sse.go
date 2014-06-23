package htt

import (
	"bytes"
	"fmt"

	"github.com/benburkert/http"
)

func SetupSSE(_ http.HandlerFunc, w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(200)
	w.Write([]byte(SSEShell))
}

type SSEResponse struct {
	w http.ResponseWriter
}

func SSEWriter(w http.ResponseWriter) SSEResponse {
	return SSEResponse{w}
}

func (r SSEResponse) Header() http.Header {
	return r.w.Header()
}

func (r SSEResponse) WriteHeader(status int) {
	r.w.WriteHeader(status)
}

func (r SSEResponse) Flush() {
	r.w.(http.Flusher).Flush()
}

func (r SSEResponse) Close() {
	r.writeCtrl("eof")
}

func (r SSEResponse) Write(buf []byte) (int, error) {
	count := 0
	var err error = nil

	for len(buf) > 0 {
		if i := bytes.Index(buf, []byte("0\r\n\r\n")); i != -1 {
			count, err = r.writeData(buf[0:i], count)
			r.writeCtrl("eof")
			return count, err
		} else {
			if i := bytes.IndexAny(buf, "\r\n"); i == -1 {
				return r.writeData(buf, count)
			} else if i == 0 {
				c := buf[0]
				buf = buf[1:]

				if c == '\r' {
					if err = r.writeCtrl("carriage-return"); err != nil {
						return count, err
					}
				} else {
					if err = r.writeCtrl("newline"); err != nil {
						return count, err
					}
				}
			} else {
				count, err = r.writeData(buf[0:i], count)
				buf = buf[i:]
			}
		}
	}

	return count, err
}

func (r SSEResponse) writeCtrl(ctrl string) error {
	_, err := r.writeSSE("ctrl", []byte(ctrl), 0)
	return err
}

func (r SSEResponse) writeData(buf []byte, count int) (int, error) {
	return r.writeSSE("message", buf, count)
}

func (r SSEResponse) writeSSE(event string, buf []byte, count int) (int, error) {
	if err := r.sendEvent(event); err != nil {
		return count, err
	}

	c, err := r.sendMessage(buf)

	return c + count, err
}

func (r SSEResponse) sendEvent(event string) error {
	data := []byte(fmt.Sprintf("event: %s\n", event))

	_, err := r.w.Write(data)
	r.w.(http.Flusher).Flush()

	return err
}

func (r SSEResponse) sendMessage(buf []byte) (int, error) {
	data := []byte(fmt.Sprintf("data: %s\n\n", buf))

	count, err := r.w.Write(data)
	r.w.(http.Flusher).Flush()

	return count - 8, err
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
