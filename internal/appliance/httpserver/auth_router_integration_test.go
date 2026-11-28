package httpserver

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/tschleining/rackos/internal/appliance/admin"
	"github.com/tschleining/rackos/internal/appliance/auth"
	"github.com/tschleining/rackos/internal/appliance/database"
	"github.com/tschleining/rackos/internal/appliance/session"
)

func TestAuthRouterPersistentLifecycle(t *testing.T) {
	ctx, cancel := context.WithTimeout(
		context.Background(),
		30*time.Second,
	)
	defer cancel()

	db, err := database.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	administrators := admin.NewRepository(db)
	sessions := session.NewRepository(db)

	const password = "Synthetic-RackOS-Router-Test-42"

	passwordBytes := []byte(password)
	defer clear(passwordBytes)

	digest, err := auth.HashPassword(
		ctx,
		"admin",
		passwordBytes,
	)
	if err != nil {
		t.Fatal(err)
	}

	defer clear(digest.Salt[:])
	defer clear(digest.Hash[:])

	account, err := administrators.CreateInitial(
		ctx,
		"admin",
		digest,
	)
	if err != nil {
		t.Fatal(err)
	}

	inventory := testHTTPInventoryRecord()
	inventory.DeviceID = testPerformanceHTTPDeviceID

	handler :=
		NewAuthenticatedManagementHandlerWithAuth(
			"0.1.0-test",
			alwaysReady,
			testManagementDeviceReader(),
			testManagementPerformanceReader(),
			&stubInventoryReader{
				record: inventory,
			},
			administrators,
			sessions,
		)

	server := httptest.NewTLSServer(handler)
	defer server.Close()

	client := server.Client()
	client.Timeout = 10 * time.Second

	// Do not allow redirect handling to conceal
	// incorrect authentication routing behavior.
	client.CheckRedirect = func(
		_ *http.Request,
		_ []*http.Request,
	) error {
		return http.ErrUseLastResponse
	}

	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}

	client.Jar = jar

	baseURL, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}

	request := func(
		method string,
		path string,
		body string,
		csrf bool,
		replayToken string,
		expected int,
	) (*http.Response, []byte) {
		t.Helper()

		req, err := http.NewRequestWithContext(
			ctx,
			method,
			server.URL+path,
			strings.NewReader(body),
		)
		if err != nil {
			t.Fatal(err)
		}

		if method == http.MethodPost {
			req.Header.Set("Origin", server.URL)
		}

		if body != "" {
			req.Header.Set(
				"Content-Type",
				"application/json",
			)
		}

		if csrf {
			req.Header.Set(
				authCSRFHeaderName,
				authCSRFHeaderValue,
			)
		}

		if replayToken != "" {
			req.AddCookie(&http.Cookie{
				Name:  sessionCookieName,
				Value: replayToken,
			})
		}

		response, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()

		payload, err := io.ReadAll(response.Body)
		if err != nil {
			t.Fatal(err)
		}

		if response.StatusCode != expected {
			t.Fatalf(
				"%s %s: expected %d, got %d",
				method,
				path,
				expected,
				response.StatusCode,
			)
		}

		if response.Header.Get(
			"Cache-Control",
		) != "no-store" {
			t.Fatal("missing cache protection")
		}

		if response.Header.Get(
			"Access-Control-Allow-Origin",
		) != "" {
			t.Fatal("unexpected CORS authorization")
		}

		return response, payload
	}

	assertSessionResponse := func(
		payload []byte,
		expected session.Session,
	) {
		t.Helper()

		var result authSessionResponse

		if err := json.Unmarshal(
			payload,
			&result,
		); err != nil {
			t.Fatal(err)
		}

		if result != toAuthSessionResponse(expected) {
			t.Fatal("incorrect session response")
		}

		if strings.Contains(
			string(payload),
			expected.ID,
		) {
			t.Fatal("internal session ID was exposed")
		}
	}

	deviceID := testPerformanceHTTPDeviceID

	protectedPaths := []string{
		"/api/v1/devices",
		"/api/v1/devices/summary",
		"/api/v1/devices/" + deviceID,
		"/api/v1/devices/" + deviceID + "/inventory",
		"/api/v1/devices/" + deviceID + "/performance/latest",
		"/api/v1/devices/" + deviceID + "/performance/history",
	}

	// Phase 1: unauthenticated appliance access.
	for _, path := range []string{
		"/healthz",
		"/readyz",
		"/api/v1/version",
	} {
		request(
			http.MethodGet,
			path,
			"",
			false,
			"",
			http.StatusOK,
		)
	}

	for _, path := range protectedPaths {
		request(
			http.MethodGet,
			path,
			"",
			false,
			"",
			http.StatusUnauthorized,
		)
	}

	request(
		http.MethodGet,
		authSessionPath,
		"",
		false,
		"",
		http.StatusUnauthorized,
	)

	// Phase 2: administrator login.
	response, payload := request(
		http.MethodPost,
		authLoginPath,
		`{"username":"ADMIN","password":"`+
			password+`"}`,
		true,
		"",
		http.StatusOK,
	)

	cookies := response.Cookies()

	if len(cookies) != 1 {
		t.Fatal("login did not issue exactly one cookie")
	}

	cookie := cookies[0]

	if cookie.Name != sessionCookieName ||
		!cookie.Secure ||
		!cookie.HttpOnly ||
		cookie.SameSite != http.SameSiteStrictMode ||
		cookie.Path != "/" ||
		cookie.Domain != "" {
		t.Fatal("incorrect authentication cookie")
	}

	token := cookie.Value

	if strings.Contains(string(payload), token) ||
		strings.Contains(string(payload), password) {
		t.Fatal("login response exposed authentication secrets")
	}

	// Verify that the client cookie jar accepted
	// the session cookie.
	stored := jar.Cookies(baseURL)

	if len(stored) != 1 ||
		stored[0].Name != sessionCookieName ||
		stored[0].Value != token {
		t.Fatal("session cookie was not retained")
	}

	original, err := sessions.Validate(ctx, token)
	if err != nil {
		t.Fatal(err)
	}

	if original.AdministratorID != account.ID {
		t.Fatal("incorrect authenticated administrator")
	}

	assertSessionResponse(payload, original)

	// Phase 3: authenticated management access.
	//
	// The cookie jar supplies authentication
	// automatically for every request.
	for _, path := range protectedPaths {
		response, _ := request(
			http.MethodGet,
			path,
			"",
			false,
			"",
			http.StatusOK,
		)

		if len(response.Cookies()) != 0 {
			t.Fatal("management request modified cookie")
		}
	}

	// Ordinary management requests and session
	// polling must not extend inactivity.
	for i := 0; i < 5; i++ {
		response, payload := request(
			http.MethodGet,
			authSessionPath,
			"",
			false,
			"",
			http.StatusOK,
		)

		if len(response.Cookies()) != 0 {
			t.Fatal("session inspection modified cookie")
		}

		assertSessionResponse(payload, original)
	}

	current, err := sessions.Validate(ctx, token)
	if err != nil {
		t.Fatal(err)
	}

	if current != original {
		t.Fatal("read-only requests changed session state")
	}

	// Phase 4: explicit activity renewal.
	for time.Now().UnixMilli() <=
		original.LastActivityAt.UnixMilli() {
		select {
		case <-ctx.Done():
			t.Fatal(ctx.Err())

		case <-time.After(time.Millisecond):
		}
	}

	response, payload = request(
		http.MethodPost,
		authActivityPath,
		"",
		true,
		"",
		http.StatusOK,
	)

	if len(response.Cookies()) != 0 {
		t.Fatal("activity renewal modified cookie")
	}

	renewed, err := sessions.Validate(ctx, token)
	if err != nil {
		t.Fatal(err)
	}

	if !renewed.LastActivityAt.After(
		original.LastActivityAt,
	) {
		t.Fatal("session activity was not renewed")
	}

	if !renewed.ExpiresAt.Equal(
		original.ExpiresAt,
	) {
		t.Fatal("absolute expiration was extended")
	}

	if renewed.ID != original.ID {
		t.Fatal("activity renewal replaced session identity")
	}

	assertSessionResponse(payload, renewed)

	// Phase 5: authenticated logout.
	response, payload = request(
		http.MethodPost,
		authLogoutPath,
		"",
		true,
		"",
		http.StatusNoContent,
	)

	if len(payload) != 0 {
		t.Fatal("logout returned a response body")
	}

	cookies = response.Cookies()

	if len(cookies) != 1 ||
		cookies[0].Name != sessionCookieName ||
		cookies[0].Value != "" ||
		cookies[0].MaxAge >= 0 {
		t.Fatal("logout did not issue deletion cookie")
	}

	if len(jar.Cookies(baseURL)) != 0 {
		t.Fatal("client retained revoked session cookie")
	}

	// Phase 6: access after logout.
	request(
		http.MethodGet,
		authSessionPath,
		"",
		false,
		"",
		http.StatusUnauthorized,
	)

	for _, path := range protectedPaths {
		request(
			http.MethodGet,
			path,
			"",
			false,
			"",
			http.StatusUnauthorized,
		)
	}

	// Explicitly replay the original token.
	// Cookie deletion alone must not be the
	// reason authentication fails.
	request(
		http.MethodGet,
		authSessionPath,
		"",
		false,
		token,
		http.StatusUnauthorized,
	)

	request(
		http.MethodPost,
		authActivityPath,
		"",
		true,
		token,
		http.StatusUnauthorized,
	)

	request(
		http.MethodPost,
		authLogoutPath,
		"",
		true,
		token,
		http.StatusUnauthorized,
	)

	_, err = sessions.Validate(ctx, token)

	if !errors.Is(err, session.ErrInvalidSession) {
		t.Fatal("revoked token remained valid")
	}

	// Phase 7: verify persistent state.
	var count int

	err = db.QueryRowContext(
		ctx,
		`SELECT COUNT(*)
		 FROM administrator_sessions`,
	).Scan(&count)
	if err != nil {
		t.Fatal(err)
	}

	if count != 0 {
		t.Fatal("logout left persistent sessions")
	}

	for _, eventType := range []string{
		"auth.login.succeeded",
		"auth.logout",
	} {
		err = db.QueryRowContext(
			ctx,
			`SELECT COUNT(*)
			 FROM audit_events
			 WHERE event_type = ?
			   AND actor_id = ?
			   AND target_id = ?`,
			eventType,
			account.ID,
			original.ID,
		).Scan(&count)
		if err != nil {
			t.Fatal(err)
		}

		if count != 1 {
			t.Fatalf(
				"expected one %s audit event, got %d",
				eventType,
				count,
			)
		}
	}

	// Public management endpoints must remain
	// accessible after administrator logout.
	request(
		http.MethodGet,
		"/healthz",
		"",
		false,
		"",
		http.StatusOK,
	)
}
