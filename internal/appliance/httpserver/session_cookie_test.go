package httpserver

import (
	"errors"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/tschleining/rackos/internal/appliance/session"
)

// Synthetic canonical token used exclusively for
// cookie parsing and transport tests.
//
// This token is not associated with a real session.
var sessionCookieTestToken = strings.Repeat(
	"A",
	session.EncodedTokenSize,
)

func newSessionCookieTestRequest() *http.Request {
	return httptest.NewRequest(
		http.MethodGet,
		"https://127.0.0.1:8080/api/v1/auth/session",
		nil,
	)
}

func assertSessionCookieAttributes(
	t *testing.T,
	cookie *http.Cookie,
) {
	t.Helper()

	if cookie.Name != sessionCookieName {
		t.Fatal("incorrect session cookie name")
	}

	if cookie.Path != "/" {
		t.Fatal("incorrect session cookie path")
	}

	if cookie.Domain != "" {
		t.Fatal("session cookie must be host-only")
	}

	if !cookie.Secure {
		t.Fatal("session cookie is missing Secure")
	}

	if !cookie.HttpOnly {
		t.Fatal("session cookie is missing HttpOnly")
	}

	if cookie.SameSite != http.SameSiteStrictMode {
		t.Fatal("session cookie must use SameSite Strict")
	}
}

func TestSessionCookieRead(t *testing.T) {
	tests := []struct {
		name    string
		headers []string
	}{
		{
			name: "single session cookie",
			headers: []string{
				sessionCookieName + "=" +
					sessionCookieTestToken,
			},
		},
		{
			name: "unrelated cookies",
			headers: []string{
				"theme=dark; " +
					sessionCookieName + "=" +
					sessionCookieTestToken +
					"; language=en",
			},
		},
		{
			name: "multiple cookie headers",
			headers: []string{
				"theme=dark",
				sessionCookieName + "=" +
					sessionCookieTestToken,
				"language=en",
			},
		},
		{
			name: "case sensitive cookie name",
			headers: []string{
				"__host-rackos_session=ignored; " +
					sessionCookieName + "=" +
					sessionCookieTestToken,
			},
		},
		{
			name: "separator whitespace",
			headers: []string{
				"theme=dark;\t " +
					sessionCookieName + "=" +
					sessionCookieTestToken,
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			request := newSessionCookieTestRequest()

			for _, header := range tc.headers {
				request.Header.Add(
					"Cookie",
					header,
				)
			}

			token, err := readSessionCookie(request)
			if err != nil {
				t.Fatal(err)
			}

			if token != sessionCookieTestToken {
				t.Fatal("incorrect authentication token")
			}
		})
	}
}

func TestSessionCookieMissing(t *testing.T) {
	tests := []string{
		"",
		"theme=dark",
		"__host-rackos_session=" +
			sessionCookieTestToken,
		"rackos_session=" +
			sessionCookieTestToken,
	}

	for _, header := range tests {
		request := newSessionCookieTestRequest()

		if header != "" {
			request.Header.Set("Cookie", header)
		}

		token, err := readSessionCookie(request)

		if !errors.Is(
			err,
			errSessionCookieMissing,
		) {
			t.Fatalf("unexpected error: %v", err)
		}

		if token != "" {
			t.Fatal("missing cookie returned a token")
		}
	}
}

func TestSessionCookieRejectsMalformedTokens(
	t *testing.T,
) {
	tests := []struct {
		name  string
		value string
	}{
		{"empty", ""},
		{"short", sessionCookieTestToken[:42]},
		{"long", sessionCookieTestToken + "A"},
		{"padded", sessionCookieTestToken + "="},
		{"quoted", `"` + sessionCookieTestToken + `"`},
		{"leading space", " " + sessionCookieTestToken},
		{"trailing space", sessionCookieTestToken + " "},
		{"leading tab", "\t" + sessionCookieTestToken},
		{"invalid character", "!" + sessionCookieTestToken[1:]},
		{"standard Base64 plus", "+" + sessionCookieTestToken[1:]},
		{"standard Base64 slash", "/" + sessionCookieTestToken[1:]},
		{"URL encoded", "%41" + sessionCookieTestToken[1:]},
		{
			"noncanonical trailing bits",
			sessionCookieTestToken[:42] + "B",
		},
		{
			"oversized",
			strings.Repeat("A", 4096),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			request := newSessionCookieTestRequest()

			request.Header.Set(
				"Cookie",
				sessionCookieName+"="+tc.value,
			)

			token, err := readSessionCookie(request)

			if !errors.Is(
				err,
				errSessionCookieInvalid,
			) {
				t.Fatalf("unexpected error: %v", err)
			}

			if token != "" {
				t.Fatal("malformed cookie returned a token")
			}

			if strings.Contains(
				err.Error(),
				sessionCookieTestToken,
			) {
				t.Fatal("authentication token leaked in error")
			}
		})
	}
}

func TestSessionCookieRejectsDuplicateCookies(
	t *testing.T,
) {
	validCookie := sessionCookieName +
		"=" + sessionCookieTestToken

	tests := []struct {
		name    string
		headers []string
	}{
		{
			name: "duplicate in same header",
			headers: []string{
				validCookie + "; " + validCookie,
			},
		},
		{
			name: "duplicate across headers",
			headers: []string{
				validCookie,
				validCookie,
			},
		},
		{
			name: "malformed duplicate first",
			headers: []string{
				sessionCookieName +
					"=invalid; " + validCookie,
			},
		},
		{
			name: "malformed duplicate last",
			headers: []string{
				validCookie + "; " +
					sessionCookieName + "=invalid",
			},
		},
		{
			name: "empty duplicate",
			headers: []string{
				validCookie + "; " +
					sessionCookieName + "=",
			},
		},
		{
			name: "duplicate without equals",
			headers: []string{
				validCookie + "; " +
					sessionCookieName,
			},
		},
		{
			name: "duplicate with malformed name",
			headers: []string{
				validCookie + "; " +
					sessionCookieName +
					" =" + sessionCookieTestToken,
			},
		},
		{
			name: "comma combined cookies",
			headers: []string{
				validCookie + ", " + validCookie,
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			request := newSessionCookieTestRequest()

			for _, header := range tc.headers {
				request.Header.Add(
					"Cookie",
					header,
				)
			}

			token, err := readSessionCookie(request)

			if !errors.Is(
				err,
				errSessionCookieInvalid,
			) {
				t.Fatalf("unexpected error: %v", err)
			}

			if token != "" {
				t.Fatal("duplicate cookies returned a token")
			}
		})
	}
}

func TestSessionCookieRejectsAlternateCredentials(
	t *testing.T,
) {
	request := newSessionCookieTestRequest()

	request.Header.Set(
		"Authorization",
		"Bearer "+sessionCookieTestToken,
	)

	request.Header.Set(
		"X-Session-Token",
		sessionCookieTestToken,
	)

	query := request.URL.Query()
	query.Set("token", sessionCookieTestToken)

	request.URL.RawQuery = query.Encode()

	token, err := readSessionCookie(request)

	if !errors.Is(
		err,
		errSessionCookieMissing,
	) {
		t.Fatalf("unexpected error: %v", err)
	}

	if token != "" {
		t.Fatal("alternate authentication credential accepted")
	}
}

func TestSessionCookieSet(t *testing.T) {
	request := newSessionCookieTestRequest()
	recorder := httptest.NewRecorder()

	recorder.Header().Set(
		"Cache-Control",
		"public, max-age=3600",
	)

	err := setSessionCookie(
		recorder,
		request,
		sessionCookieTestToken,
	)
	if err != nil {
		t.Fatal(err)
	}

	response := recorder.Result()
	defer response.Body.Close()

	cookies := response.Cookies()

	if len(cookies) != 1 {
		t.Fatal("expected exactly one session cookie")
	}

	cookie := cookies[0]

	assertSessionCookieAttributes(t, cookie)

	if cookie.Value != sessionCookieTestToken {
		t.Fatal("incorrect session cookie value")
	}

	if !cookie.Expires.IsZero() {
		t.Fatal("session cookie must not be persistent")
	}

	if cookie.MaxAge != 0 {
		t.Fatal("session cookie must not have a Max-Age")
	}

	if response.Header.Get(
		"Cache-Control",
	) != "no-store" {
		t.Fatal("authentication response is cacheable")
	}

	raw := response.Header.Get("Set-Cookie")

	if strings.Contains(
		strings.ToLower(raw),
		"domain=",
	) {
		t.Fatal("cookie contains a Domain attribute")
	}

	if strings.Contains(raw, "Max-Age=") ||
		strings.Contains(raw, "Expires=") {
		t.Fatal("cookie contains persistent expiration")
	}
}

func TestSessionCookieSetRejectsInvalidToken(
	t *testing.T,
) {
	tests := []string{
		"",
		"invalid",
		sessionCookieTestToken + "=",
		sessionCookieTestToken + "\r\n",
		`"` + sessionCookieTestToken + `"`,
	}

	for _, token := range tests {
		request := newSessionCookieTestRequest()
		recorder := httptest.NewRecorder()

		err := setSessionCookie(
			recorder,
			request,
			token,
		)

		if !errors.Is(
			err,
			errSessionCookieInvalid,
		) {
			t.Fatalf("unexpected error: %v", err)
		}

		if len(
			recorder.Header().Values("Set-Cookie"),
		) != 0 {
			t.Fatal("invalid token produced a cookie")
		}
	}
}

func TestSessionCookieClear(t *testing.T) {
	request := newSessionCookieTestRequest()
	recorder := httptest.NewRecorder()

	err := clearSessionCookie(
		recorder,
		request,
	)
	if err != nil {
		t.Fatal(err)
	}

	response := recorder.Result()
	defer response.Body.Close()

	cookies := response.Cookies()

	if len(cookies) != 1 {
		t.Fatal("expected exactly one deletion cookie")
	}

	cookie := cookies[0]

	assertSessionCookieAttributes(t, cookie)

	if cookie.Value != "" {
		t.Fatal("deletion cookie contains a token")
	}

	if cookie.MaxAge != -1 {
		t.Fatal("deletion cookie must immediately expire")
	}

	if cookie.Expires.IsZero() ||
		!cookie.Expires.Before(time.Now()) {
		t.Fatal("deletion cookie expiration is incorrect")
	}

	if !strings.Contains(
		response.Header.Get("Set-Cookie"),
		"Max-Age=0",
	) {
		t.Fatal("deletion cookie missing Max-Age=0")
	}

	if response.Header.Get(
		"Cache-Control",
	) != "no-store" {
		t.Fatal("cookie deletion response is cacheable")
	}
}

func TestSessionCookieRequiresHTTPS(t *testing.T) {
	request := httptest.NewRequest(
		http.MethodGet,
		"http://127.0.0.1:8080/api/v1/auth/session",
		nil,
	)

	request.Header.Set(
		"Cookie",
		sessionCookieName+"="+sessionCookieTestToken,
	)

	// Proxy headers must not convert a plaintext
	// request into an authenticated HTTPS request.
	request.Header.Set(
		"X-Forwarded-Proto",
		"https",
	)

	request.Header.Set(
		"Forwarded",
		"proto=https",
	)

	request.Header.Set(
		"X-Forwarded-Ssl",
		"on",
	)

	token, err := readSessionCookie(request)

	if !errors.Is(
		err,
		errSessionHTTPSRequired,
	) {
		t.Fatalf("unexpected read error: %v", err)
	}

	if token != "" {
		t.Fatal("HTTP request returned authentication token")
	}

	recorder := httptest.NewRecorder()

	err = setSessionCookie(
		recorder,
		request,
		sessionCookieTestToken,
	)

	if !errors.Is(
		err,
		errSessionHTTPSRequired,
	) {
		t.Fatalf("unexpected issuance error: %v", err)
	}

	err = clearSessionCookie(
		recorder,
		request,
	)

	if !errors.Is(
		err,
		errSessionHTTPSRequired,
	) {
		t.Fatalf("unexpected deletion error: %v", err)
	}

	if len(
		recorder.Header().Values("Set-Cookie"),
	) != 0 {
		t.Fatal("plaintext response included Set-Cookie")
	}
}

func TestSessionCookieHTTPSRoundTrip(t *testing.T) {
	handler := http.HandlerFunc(func(
		w http.ResponseWriter,
		r *http.Request,
	) {
		switch r.URL.Path {
		case "/test/issue":
			if err := setSessionCookie(
				w,
				r,
				sessionCookieTestToken,
			); err != nil {
				w.WriteHeader(
					http.StatusInternalServerError,
				)
				return
			}

		case "/test/read":
			token, err := readSessionCookie(r)

			if err != nil ||
				token != sessionCookieTestToken {
				w.WriteHeader(
					http.StatusUnauthorized,
				)
				return
			}

		case "/test/clear":
			if err := clearSessionCookie(
				w,
				r,
			); err != nil {
				w.WriteHeader(
					http.StatusInternalServerError,
				)
				return
			}

		default:
			w.WriteHeader(http.StatusNotFound)
			return
		}

		w.WriteHeader(http.StatusNoContent)
	})

	server := httptest.NewTLSServer(handler)
	defer server.Close()

	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}

	// Use the client's existing trusted test TLS
	// configuration rather than disabling certificate
	// verification.
	client := server.Client()
	client.Jar = jar

	request := func(
		path string,
		expectedStatus int,
	) {
		t.Helper()

		response, err := client.Get(
			server.URL + path,
		)
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()

		if response.StatusCode != expectedStatus {
			t.Fatalf(
				"%s: expected status %d, got %d",
				path,
				expectedStatus,
				response.StatusCode,
			)
		}
	}

	// No cookie is present initially.
	request("/test/read", http.StatusUnauthorized)

	// Receive a cookie over HTTPS.
	request("/test/issue", http.StatusNoContent)

	// The cookie jar automatically sends it
	// with subsequent HTTPS requests.
	request("/test/read", http.StatusNoContent)

	// Remove the cookie.
	request("/test/clear", http.StatusNoContent)

	// The client must no longer send it.
	request("/test/read", http.StatusUnauthorized)
}
