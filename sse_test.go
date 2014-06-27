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

	assertEqual("abc", "data:abc\n\n")
	assertEqual("a\nb\tc\rd", "data:a%0Ab%09c%0Dd\n\n")
	assertEqual("data:abc\n", "data:data%3Aabc%0A\n\n")
	assertEqual("☃", "data:%E2%98%83\n\n")
}
