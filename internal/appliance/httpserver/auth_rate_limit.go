package httpserver

import (
	"net/http"
	"strconv"
	"sync"
	"time"
)

const (
	loginMaxAttempts = 5
	loginRateWindow  = 60 * time.Second
	loginBusyRetry   = time.Second
)

// loginRateLimiter enforces an appliance-wide
// rolling login attempt limit.
//
// Its memory usage is fixed and independent of
// usernames, client addresses and session cookies.
//
// A limiter must be shared by all login requests
// handled by the appliance.
type loginRateLimiter struct {
	mu sync.Mutex

	attempts [loginMaxAttempts]time.Time
	count    int
	inFlight bool

	lastObserved time.Time

	// Optional clock override for deterministic tests.
	// Configure before using the limiter.
	now func() time.Time
}

func newLoginRateLimiter() *loginRateLimiter {
	return &loginRateLimiter{
		now: time.Now,
	}
}

// acquire reserves capacity for one login operation.
//
// Successful admission consumes an attempt regardless
// of whether authentication subsequently succeeds.
//
// The caller must invoke the returned release
// function when the operation completes.
//
// Rejected requests do not consume additional
// attempts or extend the rate limiting window.
func (l *loginRateLimiter) acquire() (
	release func(),
	retryAfter time.Duration,
) {
	l.mu.Lock()
	defer l.mu.Unlock()

	clock := l.now
	if clock == nil {
		clock = time.Now
	}

	// Sample the clock while holding the lock so
	// concurrent requests cannot observe timestamps
	// in an inconsistent order.
	now := clock()

	// Fail closed if the clock moves backward.
	// Production time.Now values include monotonic
	// clock information when available.
	if !l.lastObserved.IsZero() &&
		now.Before(l.lastObserved) {
		return nil, loginBusyRetry
	}

	l.lastObserved = now

	cutoff := now.Add(-loginRateWindow)

	// Remove attempts that have reached the
	// exact rolling-window expiration boundary.
	expired := 0

	for expired < l.count {
		if l.attempts[expired].After(cutoff) {
			break
		}

		expired++
	}

	if expired > 0 {
		remaining := l.count - expired

		copy(
			l.attempts[:remaining],
			l.attempts[expired:l.count],
		)

		clear(l.attempts[remaining:])

		l.count = remaining
	}

	if l.count >= loginMaxAttempts {
		retry := l.attempts[0].
			Add(loginRateWindow).
			Sub(now)

		return nil, retry
	}

	// Do not queue concurrent login operations.
	// Reject excess work before password verification.
	if l.inFlight {
		return nil, loginBusyRetry
	}

	l.attempts[l.count] = now
	l.count++
	l.inFlight = true

	// A release function may safely be called more
	// than once without releasing another operation.
	var once sync.Once

	return func() {
		once.Do(func() {
			l.mu.Lock()
			defer l.mu.Unlock()

			l.inFlight = false
		})
	}, 0
}

// requireLoginCapacity limits login attempts before
// request decoding and password verification.
//
// Local management security and CSRF checks must
// execute before this middleware.
func requireLoginCapacity(
	limiter *loginRateLimiter,
	next http.Handler,
) http.Handler {
	return http.HandlerFunc(func(
		w http.ResponseWriter,
		r *http.Request,
	) {
		w.Header().Set(
			"Cache-Control",
			"no-store",
		)

		if limiter == nil {
			writeAuthenticationUnavailable(w)
			return
		}

		release, retryAfter := limiter.acquire()

		if release == nil {
			writeLoginRateLimit(
				w,
				retryAfter,
			)
			return
		}

		defer release()

		next.ServeHTTP(w, r)
	})
}

// writeLoginRateLimit returns a generic rejection
// without disclosing administrator account state.
func writeLoginRateLimit(
	w http.ResponseWriter,
	retryAfter time.Duration,
) {
	// Retry-After uses whole seconds. Round upward
	// so clients do not retry before capacity returns.
	seconds := retryAfter / time.Second

	if retryAfter%time.Second != 0 {
		seconds++
	}

	if seconds < 1 {
		seconds = 1
	}

	w.Header().Set(
		"Cache-Control",
		"no-store",
	)

	w.Header().Set(
		"Retry-After",
		strconv.FormatInt(int64(seconds), 10),
	)

	writeDeviceError(
		w,
		http.StatusTooManyRequests,
		"too many authentication attempts",
	)
}
