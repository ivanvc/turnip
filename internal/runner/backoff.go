package runner

import (
	"math"
	"time"
)

// nextDelay computes a full-jitter exponential backoff delay for the given
// zero-based attempt number: rand(0, min(max, base*2^attempt)) — the
// standard shape for avoiding a thundering herd of reconnecting clients
// (Requirement 2.6). rnd must return a value in [0, 1).
func nextDelay(attempt int, base, max time.Duration, rnd func() float64) time.Duration {
	backoff := float64(base) * math.Pow(2, float64(attempt))
	if cap := float64(max); backoff > cap {
		backoff = cap
	}
	return time.Duration(rnd() * backoff)
}

// retrier repeats a call to fn, applying nextDelay's backoff between
// attempts, until fn succeeds or the total elapsed time (measured via now)
// exceeds budget. now and sleep are seams — a real time.Sleep never
// appears directly in this file — so tests never wait on wall-clock time.
type retrier struct {
	base, max, budget time.Duration
	now               func() time.Time
	sleep             func(time.Duration)
	rnd               func() float64
}

// run calls fn repeatedly until it returns nil or the budget is exhausted.
// A non-nil return is always fn's most recent error — run only ever gives
// up once retrying further would exceed budget.
func (r *retrier) run(fn func() error) error {
	start := r.now()

	var lastErr error
	for attempt := 0; ; attempt++ {
		lastErr = fn()
		if lastErr == nil {
			return nil
		}
		if r.now().Sub(start) >= r.budget {
			return lastErr
		}
		r.sleep(nextDelay(attempt, r.base, r.max, r.rnd))
	}
}
