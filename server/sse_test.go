package server

import (
	"io"
	"testing"
)

func TestSSEData(t *testing.T) {
	r, w := io.Pipe()
	sw := sseWriter{w}

	assertEqual := func(before, after string) {
		go func() { sw.Write([]byte(before)) }()

		result := make([]byte, len(after))

		r.Read(result)

		if string(result) != after {
			t.Errorf("SSE formatted data is %q, want %q", string(result), after)
		}
	}

	assertEqual("abc", "data:\"abc\"\n\n")
	assertEqual("a\nb\tc\rd", "data:\"a\\nb\\u0009c\\rd\"\n\n")
	assertEqual("data:abc\n", "data:\"data:abc\\n\"\n\n")
	assertEqual("☃", "data:\"☃\"\n\n")
}
