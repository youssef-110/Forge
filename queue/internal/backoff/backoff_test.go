package backoff

import (
	"testing"
	"time"
)

func TestFullJitterBounds(t *testing.T) {
	base, cap := time.Second, 60*time.Second
	cases := map[int]time.Duration{1: time.Second, 2: 2 * time.Second, 3: 4 * time.Second, 7: cap, 100: cap}
	for attempt, ceiling := range cases {
		var hi time.Duration
		for i := 0; i < 2000; i++ {
			d := FullJitter(attempt, base, cap)
			if d < 0 || d >= ceiling {
				t.Fatalf("attempt %d: %v outside [0,%v)", attempt, d, ceiling)
			}
			hi = max(hi, d)
		}
		if hi < ceiling/2 { // with 2000 samples this only fails if the range is wrong
			t.Fatalf("attempt %d: max %v never got near %v", attempt, hi, ceiling)
		}
	}
}
