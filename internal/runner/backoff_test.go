package runner

import (
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNextDelay_StaysWithinZeroToMax(t *testing.T) {
	base, max := time.Second, 30*time.Second
	rnds := []float64{0, 0.5, 0.999}

	for attempt := 0; attempt < 10; attempt++ {
		for _, rnd := range rnds {
			r := rnd
			d := nextDelay(attempt, base, max, func() float64 { return r })
			assert.GreaterOrEqualf(t, d, time.Duration(0), "attempt=%d rnd=%v", attempt, r)
			assert.LessOrEqualf(t, d, max, "attempt=%d rnd=%v", attempt, r)
		}
	}
}

func TestNextDelay_GrowsExponentiallyUpToCap(t *testing.T) {
	base, max := time.Second, 30*time.Second
	// With rnd always returning 1 (the theoretical upper bound), the delay
	// is exactly min(max, base*2^attempt): strictly increasing until it
	// saturates at max.
	rnd := func() float64 { return 1 }

	prev := time.Duration(0)
	sawCap := false
	for attempt := 0; attempt < 10; attempt++ {
		d := nextDelay(attempt, base, max, rnd)
		if d == max {
			sawCap = true
		}
		assert.GreaterOrEqualf(t, d, prev, "attempt=%d", attempt)
		prev = d
	}
	assert.True(t, sawCap, "expected the delay to reach the cap within 10 attempts")
}

func TestRetrier_StopsOnSuccess(t *testing.T) {
	calls := 0
	r := &retrier{
		base: time.Millisecond, max: time.Millisecond, budget: time.Hour,
		now: time.Now, sleep: func(time.Duration) {}, rnd: func() float64 { return 0 },
	}

	err := r.run(func() error {
		calls++
		if calls == 3 {
			return nil
		}
		return errors.New("not yet")
	})
	require.NoError(t, err)
	assert.Equal(t, 3, calls)
}

func TestRetrier_StopsOnceBudgetElapses(t *testing.T) {
	budget := 10 * time.Second
	now := time.Now()
	var sleeps []time.Duration

	r := &retrier{
		base: time.Second, max: time.Second, budget: budget,
		now: func() time.Time { return now },
		sleep: func(d time.Duration) {
			sleeps = append(sleeps, d)
			now = now.Add(d)
		},
		rnd: func() float64 { return 1 },
	}

	wantErr := errors.New("always fails")
	calls := 0
	err := r.run(func() error {
		calls++
		return wantErr
	})

	require.ErrorIs(t, err, wantErr)
	assert.Greater(t, calls, 1, "should have retried at least once")

	var elapsed time.Duration
	for _, d := range sleeps {
		elapsed += d
	}
	// The loop only checks the budget between attempts, so it may run one
	// more attempt past the budget but never sleep past it by more than
	// one attempt's delay (bounded by max here).
	assert.LessOrEqual(t, elapsed, budget+time.Second)
}
