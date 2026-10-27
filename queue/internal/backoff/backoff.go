// Package backoff computes retry delays.
package backoff

import (
	"math/rand/v2"
	"time"
)

// FullJitter returns a delay in [0, min(cap, base*2^(attempt-1))).
// Full jitter spreads retries of jobs that failed together, so they do not hit a
// recovering dependency in synchronized waves.
func FullJitter(attempt int, base, cap time.Duration) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	ceiling := cap
	if attempt <= 32 { // avoid shift overflow; 2^32 * base is past any sane cap
		if d := base << (attempt - 1); d > 0 && d < cap {
			ceiling = d
		}
	}
	if ceiling <= 0 {
		return 0
	}
	return time.Duration(rand.Int64N(int64(ceiling)))
}
