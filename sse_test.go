package htee

import "testing"

func TestSSEData(t *testing.T) {
	uc := make(chan []byte)
	ec := formatSSEData(uc)

	assertEqual := func(before, after string) {
		uc <- []byte(before)

		if result := <-ec; result != after {
			t.Errorf("SSE formatted data is %q, want %q", result, after)
		}
	}

	assertEqual("abc", "data:abc\n")
	assertEqual("a\nb\tc", "data:a\\nb\\tc\n")
	assertEqual("data:abc\n", "data:data:abc\\n\n")
}
