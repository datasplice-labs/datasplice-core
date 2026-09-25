package httpengine

import (
	"context"
	"fmt"
	"math/rand/v2"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/datasplice-labs/datasplice-core/internal/manifest"
)

const (
	defaultMaxAttempts  = 3
	defaultInitialDelay = time.Second
	maxBackoff          = time.Minute
)

// retryPolicy is manifest.Retry with defaults applied. The zero value
// never retries.
type retryPolicy struct {
	on                map[int]bool
	respectRetryAfter bool
	maxAttempts       int
	backoff           string
	initialDelay      time.Duration
}

func newRetryPolicy(r *manifest.Retry) retryPolicy {
	p := retryPolicy{maxAttempts: 1}
	if r == nil {
		return p
	}

	p.on = make(map[int]bool, len(r.On))
	for _, code := range r.On {
		p.on[code] = true
	}

	p.respectRetryAfter = r.RespectRetryAfter
	p.backoff = r.Backoff
	p.maxAttempts = r.MaxAttempts
	if p.maxAttempts == 0 {
		p.maxAttempts = defaultMaxAttempts
	}

	p.initialDelay = defaultInitialDelay
	if r.InitialDelay != "" {
		// Load already checked this parses.
		p.initialDelay, _ = time.ParseDuration(r.InitialDelay)
	}

	return p
}

func (p retryPolicy) shouldRetry(status int) bool { return p.on[status] }

// delay is how long to wait before attempt+1. A server-provided
// Retry-After wins (when the manifest respects it); otherwise the
// manifest's backoff applies.
func (p retryPolicy) delay(attempt int, h http.Header, now time.Time) time.Duration {
	if p.respectRetryAfter {
		if d, ok := parseRetryAfter(h.Get("Retry-After"), now); ok {
			return d
		}
	}

	switch p.backoff {
	case "fixed":
		return p.initialDelay
	case "linear":
		return min(p.initialDelay*time.Duration(attempt), maxBackoff)
	}

	// Exponential, with "equal jitter": half the delay is fixed and half
	// random, so retries from parallel runs spread out without ever
	// shrinking below half of the previous attempt's delay.
	d := p.initialDelay
	for i := 1; i < attempt && d < maxBackoff; i++ {
		d *= 2
	}
	d = min(d, maxBackoff)

	return d/2 + time.Duration(rand.Int64N(int64(d/2)+1))
}

// parseRetryAfter reads either form of the header: whole seconds, or an
// HTTP date.
func parseRetryAfter(v string, now time.Time) (time.Duration, bool) {
	v = strings.TrimSpace(v)
	if v == "" {
		return 0, false
	}

	if secs, err := strconv.Atoi(v); err == nil && secs >= 0 {
		return time.Duration(secs) * time.Second, true
	}

	if t, err := http.ParseTime(v); err == nil {
		return max(t.Sub(now), 0), true
	}

	return 0, false
}

// sleepCtx waits d, returning early with ctx.Err() if ctx is cancelled.
func sleepCtx(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()

	select {
	case <-t.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// RequestError is a request that failed for good: a non-retryable
// status, or retries ran out. URL never includes query-string auth.
type RequestError struct {
	Method   string
	URL      string
	Status   int
	Attempts int
	Emitted  int // records already passed downstream when it failed
	Body     string
	Err      error
}

func (e *RequestError) Error() string {
	msg := fmt.Sprintf("%s %s: ", e.Method, e.URL)
	if e.Err != nil {
		msg += e.Err.Error()
	} else {
		msg += fmt.Sprintf("status %d", e.Status)
	}

	msg += fmt.Sprintf(" after %d attempt(s), %d records emitted", e.Attempts, e.Emitted)
	if e.Body != "" {
		msg += ": " + e.Body
	}

	return msg
}

func (e *RequestError) Unwrap() error { return e.Err }
