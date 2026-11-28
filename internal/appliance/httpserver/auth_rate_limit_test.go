package httpserver

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestLoginRateLimiterCapacity(t *testing.T) {
	now := time.Date(
		2026, time.November, 27,
		12, 0, 0, 0, time.UTC,
	)

	limiter := &loginRateLimiter{
		now: func() time.Time {
			return now
		},
	}

	for i := 0; i < loginMaxAttempts; i++ {
		release, retry := limiter.acquire()

		if release == nil || retry != 0 {
			t.Fatalf(
				"attempt %d unexpectedly rejected",
				i+1,
			)
		}

		release()
	}

	release, retry := limiter.acquire()

	if release != nil {
		release()
		t.Fatal("rate limit was not enforced")
	}

	if retry != loginRateWindow {
		t.Fatalf(
			"incorrect retry interval: %v",
			retry,
		)
	}

	// Requests just before the expiration
	// boundary must remain rejected.
	now = now.Add(
		loginRateWindow - time.Millisecond,
	)

	release, retry = limiter.acquire()

	if release != nil {
		release()
		t.Fatal("attempt admitted before expiration")
	}

	if retry != time.Millisecond {
		t.Fatalf(
			"incorrect remaining interval: %v",
			retry,
		)
	}

	// Capacity returns at the exact boundary.
	now = now.Add(time.Millisecond)

	release, retry = limiter.acquire()

	if release == nil || retry != 0 {
		t.Fatal("capacity did not recover")
	}

	release()
}

func TestLoginRateLimiterRollingWindow(t *testing.T) {
	now := time.Date(
		2026, time.November, 27,
		12, 0, 0, 0, time.UTC,
	)

	limiter := &loginRateLimiter{
		now: func() time.Time {
			return now
		},
	}

	// Admit one attempt every ten seconds.
	for i := 0; i < loginMaxAttempts; i++ {
		release, retry := limiter.acquire()

		if release == nil || retry != 0 {
			t.Fatalf(
				"attempt %d unexpectedly rejected",
				i+1,
			)
		}

		release()
		now = now.Add(10 * time.Second)
	}

	release, retry := limiter.acquire()

	if release != nil {
		release()
		t.Fatal("rolling window limit not enforced")
	}

	if retry != 10*time.Second {
		t.Fatalf(
			"incorrect retry interval: %v",
			retry,
		)
	}

	// Only the oldest attempt should expire.
	now = now.Add(10 * time.Second)

	release, retry = limiter.acquire()

	if release == nil || retry != 0 {
		t.Fatal("expired attempt did not release capacity")
	}

	release()

	release, retry = limiter.acquire()

	if release != nil {
		release()
		t.Fatal("rolling window reset all attempts")
	}

	if retry != 10*time.Second {
		t.Fatalf(
			"incorrect next retry interval: %v",
			retry,
		)
	}
}

func TestLoginRateLimiterConcurrentOperation(
	t *testing.T,
) {
	limiter := newLoginRateLimiter()

	release, retry := limiter.acquire()

	if release == nil || retry != 0 {
		t.Fatal("initial operation rejected")
	}

	defer release()

	// Concurrent rejections must not exhaust
	// the rolling attempt budget.
	for i := 0; i < 100; i++ {
		otherRelease, retry := limiter.acquire()

		if otherRelease != nil {
			otherRelease()
			t.Fatal("concurrent operation admitted")
		}

		if retry != loginBusyRetry {
			t.Fatalf(
				"incorrect busy retry: %v",
				retry,
			)
		}
	}

	release()

	// Four attempts should remain available.
	for i := 1; i < loginMaxAttempts; i++ {
		nextRelease, retry := limiter.acquire()

		if nextRelease == nil || retry != 0 {
			t.Fatal("busy requests consumed capacity")
		}

		nextRelease()
	}

	nextRelease, _ := limiter.acquire()

	if nextRelease != nil {
		nextRelease()
		t.Fatal("attempt budget was not enforced")
	}
}

func TestLoginRateLimiterConcurrentRequests(
	t *testing.T,
) {
	now := time.Date(
		2026, time.November, 27,
		12, 0, 0, 0, time.UTC,
	)

	limiter := &loginRateLimiter{
		now: func() time.Time {
			return now
		},
	}

	const requests = 64

	start := make(chan struct{})
	results := make(chan func(), requests)

	var wg sync.WaitGroup

	for i := 0; i < requests; i++ {
		wg.Add(1)

		go func() {
			defer wg.Done()

			<-start

			release, _ := limiter.acquire()

			// Hold the admitted operation open
			// until all requests have attempted
			// to acquire capacity.
			results <- release
		}()
	}

	close(start)

	wg.Wait()
	close(results)

	admitted := 0

	for release := range results {
		if release != nil {
			admitted++
			defer release()
		}
	}

	if admitted != 1 {
		t.Fatalf(
			"expected one admitted operation, got %d",
			admitted,
		)
	}
}

func TestLoginRateLimiterIdempotentRelease(
	t *testing.T,
) {
	limiter := newLoginRateLimiter()

	first, _ := limiter.acquire()

	if first == nil {
		t.Fatal("first operation rejected")
	}

	first()

	second, _ := limiter.acquire()

	if second == nil {
		t.Fatal("second operation rejected")
	}

	defer second()

	// A stale release function must not release
	// the currently active operation.
	first()

	third, _ := limiter.acquire()

	if third != nil {
		third()
		t.Fatal("stale release cleared active operation")
	}
}

func TestLoginRateLimiterClockRollback(
	t *testing.T,
) {
	now := time.Date(
		2026, time.November, 27,
		12, 0, 0, 0, time.UTC,
	)

	limiter := &loginRateLimiter{
		now: func() time.Time {
			return now
		},
	}

	release, _ := limiter.acquire()

	if release == nil {
		t.Fatal("initial operation rejected")
	}

	release()

	now = now.Add(-time.Minute)

	release, retry := limiter.acquire()

	if release != nil {
		release()
		t.Fatal("clock rollback admitted an operation")
	}

	if retry <= 0 {
		t.Fatal("missing retry interval")
	}

	now = now.Add(time.Minute)

	release, retry = limiter.acquire()

	if release == nil || retry != 0 {
		t.Fatal("limiter did not recover")
	}

	release()
}

func TestLoginRateLimiterRejectedRequestsDoNotExtendWindow(
	t *testing.T,
) {
	now := time.Date(
		2026, time.November, 27,
		12, 0, 0, 0, time.UTC,
	)

	limiter := &loginRateLimiter{
		now: func() time.Time {
			return now
		},
	}

	for i := 0; i < loginMaxAttempts; i++ {
		release, _ := limiter.acquire()

		if release == nil {
			t.Fatal("initial attempt rejected")
		}

		release()
	}

	for i := 0; i < 59; i++ {
		now = now.Add(time.Second)

		release, _ := limiter.acquire()

		if release != nil {
			release()
			t.Fatal("rate-limited request admitted")
		}
	}

	now = now.Add(time.Second)

	release, retry := limiter.acquire()

	if release == nil || retry != 0 {
		t.Fatal("rejected requests extended rate window")
	}

	release()
}

func TestLoginRateLimitMiddleware(t *testing.T) {
	limiter := newLoginRateLimiter()

	calls := 0

	handler := requireLoginCapacity(
		limiter,
		http.HandlerFunc(func(
			w http.ResponseWriter,
			r *http.Request,
		) {
			calls++

			// Successful requests must not reset
			// the login attempt budget.
			w.WriteHeader(http.StatusOK)
		}),
	)

	for i := 0; i < loginMaxAttempts+1; i++ {
		request := httptest.NewRequest(
			http.MethodPost,
			"https://127.0.0.1/api/v1/auth/login",
			nil,
		)

		// Changing client identity must not
		// bypass appliance-wide rate limiting.
		request.RemoteAddr =
			"127.0.0.1:" + string(rune('1'+i))

		request.Header.Set(
			"X-Forwarded-For",
			"192.0.2."+string(rune('1'+i)),
		)

		recorder := httptest.NewRecorder()

		handler.ServeHTTP(recorder, request)

		expected := http.StatusOK

		if i >= loginMaxAttempts {
			expected = http.StatusTooManyRequests
		}

		if recorder.Code != expected {
			t.Fatalf(
				"expected %d, got %d",
				expected,
				recorder.Code,
			)
		}

		if recorder.Header().Get(
			"Cache-Control",
		) != "no-store" {
			t.Fatal("missing cache protection")
		}

		if expected == http.StatusTooManyRequests {
			if recorder.Header().Get(
				"Retry-After",
			) == "" {
				t.Fatal("missing Retry-After header")
			}

			if !strings.Contains(
				recorder.Body.String(),
				"too many authentication attempts",
			) {
				t.Fatal("incorrect rate limit response")
			}
		}
	}

	if calls != loginMaxAttempts {
		t.Fatalf(
			"unexpected downstream call count: %d",
			calls,
		)
	}
}

func TestLoginRateLimitMiddlewareMissingLimiter(
	t *testing.T,
) {
	handler := requireLoginCapacity(
		nil,
		http.HandlerFunc(func(
			w http.ResponseWriter,
			r *http.Request,
		) {
			t.Fatal("missing limiter allowed authentication")
		}),
	)

	request := httptest.NewRequest(
		http.MethodPost,
		"https://127.0.0.1/api/v1/auth/login",
		nil,
	)

	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf(
			"expected 503, got %d",
			recorder.Code,
		)
	}
}

func TestLoginRateLimitRetryAfterRounding(t *testing.T) {
	tests := []struct {
		duration time.Duration
		expected string
	}{
		{0, "1"},
		{time.Millisecond, "1"},
		{time.Second, "1"},
		{time.Second + time.Millisecond, "2"},
		{1500 * time.Millisecond, "2"},
		{59*time.Second + time.Millisecond, "60"},
		{60 * time.Second, "60"},
	}

	for _, tt := range tests {
		t.Run(tt.duration.String(), func(t *testing.T) {
			recorder := httptest.NewRecorder()

			writeLoginRateLimit(
				recorder,
				tt.duration,
			)

			if recorder.Code != http.StatusTooManyRequests {
				t.Fatal("incorrect response status")
			}

			if recorder.Header().Get(
				"Retry-After",
			) != tt.expected {
				t.Fatal("incorrect Retry-After rounding")
			}
		})
	}
}

func TestLoginRateLimitCSRFComposition(t *testing.T) {
	limiter := newLoginRateLimiter()

	calls := 0

	handler := protectLocalManagement(
		requireAuthMutationCSRF(
			requireLoginCapacity(
				limiter,
				http.HandlerFunc(func(
					w http.ResponseWriter,
					r *http.Request,
				) {
					calls++

					w.WriteHeader(
						http.StatusNoContent,
					)
				}),
			),
		),
	)

	const origin = "https://127.0.0.1:8443"
	const target = origin + "/api/v1/auth/login"

	// Requests rejected by CSRF protection must
	// not consume legitimate login capacity.
	for i := 0; i < 100; i++ {
		request := httptest.NewRequest(
			http.MethodPost,
			target,
			nil,
		)

		request.Header.Set("Origin", origin)

		recorder := httptest.NewRecorder()

		handler.ServeHTTP(recorder, request)

		if recorder.Code != http.StatusForbidden {
			t.Fatal("missing CSRF header was accepted")
		}
	}

	for i := 0; i < loginMaxAttempts; i++ {
		request := httptest.NewRequest(
			http.MethodPost,
			target,
			nil,
		)

		request.Header.Set("Origin", origin)

		request.Header.Set(
			authCSRFHeaderName,
			authCSRFHeaderValue,
		)

		recorder := httptest.NewRecorder()

		handler.ServeHTTP(recorder, request)

		if recorder.Code != http.StatusNoContent {
			t.Fatal("CSRF failures consumed login capacity")
		}
	}

	if calls != loginMaxAttempts {
		t.Fatal("unexpected handler invocation count")
	}
}
