package server

import (
	"encoding/json"
	"io"
	"net/http"
)

func isSSE(r *http.Request) bool {
	return r.Header.Get("Accept") == "text/event-stream"
}

type sseWriter struct {
	w io.Writer
}

func (w sseWriter) Write(buf []byte) (int, error) {
	if data, jerr := json.Marshal(string(buf)); jerr != nil {
		if n, werr := w.w.Write([]byte("event:error\ndata:\n\n")); werr != nil {
			return n, werr
		} else {
			return n, jerr
		}
	} else {
		message := "data:" + string(data) + "\n\n"
		return w.w.Write([]byte(message))
	}
}
