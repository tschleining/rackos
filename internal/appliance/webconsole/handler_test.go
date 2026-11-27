package webconsole

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
)

const testIndex = `<!doctype html>
<html>
<head><title>RackOS</title></head>
<body><div id="root"></div></body>
</html>`

func testAssets() fstest.MapFS {
	return fstest.MapFS{
		"index.html": &fstest.MapFile{
			Data: []byte(testIndex),
		},

		"assets/index-abcdefgh.js": &fstest.MapFile{
			Data: []byte(`console.log("RackOS");`),
		},

		"assets/index-abcdefgh.css": &fstest.MapFile{
			Data: []byte("body { margin: 0; }"),
		},

		"assets/unhashed.js": &fstest.MapFile{
			Data: []byte("export {};"),
		},

		"assets/index.js.map": &fstest.MapFile{
			Data: []byte("{}"),
		},
	}
}

func TestNewRejectsMissingIndex(t *testing.T) {
	assets := testAssets()

	delete(assets, "index.html")

	if _, err := New(assets); err == nil {
		t.Fatal("expected missing index error")
	}
}

func TestNewRejectsEmptyIndex(t *testing.T) {
	assets := testAssets()

	assets["index.html"].Data = nil

	if _, err := New(assets); err == nil {
		t.Fatal("expected empty index error")
	}
}

func TestNewRejectsMissingAssets(t *testing.T) {
	assets := fstest.MapFS{
		"index.html": &fstest.MapFile{
			Data: []byte(testIndex),
		},
	}

	if _, err := New(assets); err == nil {
		t.Fatal("expected missing assets error")
	}
}

func TestConsoleRouting(t *testing.T) {
	handler, err := New(testAssets())
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name       string
		method     string
		path       string
		accept     string
		wantStatus int
		wantBody   string
	}{
		{
			name:       "root",
			method:     http.MethodGet,
			path:       "/",
			wantStatus: http.StatusOK,
			wantBody:   testIndex,
		},
		{
			name:       "device list",
			method:     http.MethodGet,
			path:       "/devices",
			accept:     "text/html",
			wantStatus: http.StatusOK,
			wantBody:   testIndex,
		},
		{
			name:       "device performance deep link",
			method:     http.MethodGet,
			path:       "/devices/11111111-1111-4111-8111-111111111111/performance",
			accept:     "text/html",
			wantStatus: http.StatusOK,
			wantBody:   testIndex,
		},
		{
			name:       "unknown frontend route",
			method:     http.MethodGet,
			path:       "/unknown-route",
			accept:     "text/html",
			wantStatus: http.StatusOK,
			wantBody:   testIndex,
		},
		{
			name:       "non HTML request",
			method:     http.MethodGet,
			path:       "/unknown-route",
			accept:     "application/json",
			wantStatus: http.StatusNotFound,
		},
		{
			name:       "HTML explicitly rejected",
			method:     http.MethodGet,
			path:       "/devices",
			accept:     "text/html;q=0",
			wantStatus: http.StatusNotFound,
		},
		{
			name:       "javascript asset",
			method:     http.MethodGet,
			path:       "/assets/index-abcdefgh.js",
			wantStatus: http.StatusOK,
			wantBody:   `console.log("RackOS");`,
		},
		{
			name:       "missing javascript asset",
			method:     http.MethodGet,
			path:       "/assets/missing.js",
			accept:     "text/html",
			wantStatus: http.StatusNotFound,
		},
		{
			name:       "missing root file",
			method:     http.MethodGet,
			path:       "/missing.js",
			accept:     "text/html",
			wantStatus: http.StatusNotFound,
		},
		{
			name:       "directory listing",
			method:     http.MethodGet,
			path:       "/assets/",
			accept:     "text/html",
			wantStatus: http.StatusNotFound,
		},
		{
			name:       "source map denied",
			method:     http.MethodGet,
			path:       "/assets/index.js.map",
			wantStatus: http.StatusNotFound,
		},
		{
			name:       "noncanonical path",
			method:     http.MethodGet,
			path:       "/assets/../index.html",
			wantStatus: http.StatusNotFound,
		},
		{
			name:       "write request denied",
			method:     http.MethodPost,
			path:       "/",
			wantStatus: http.StatusMethodNotAllowed,
		},
		{
			name:       "HEAD request",
			method:     http.MethodHead,
			path:       "/",
			wantStatus: http.StatusOK,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			request := httptest.NewRequest(
				tc.method,
				tc.path,
				nil,
			)

			if tc.accept != "" {
				request.Header.Set(
					"Accept",
					tc.accept,
				)
			}

			response := httptest.NewRecorder()

			handler.ServeHTTP(
				response,
				request,
			)

			if response.Code != tc.wantStatus {
				t.Fatalf(
					"status = %d, want %d",
					response.Code,
					tc.wantStatus,
				)
			}

			if tc.wantBody != "" &&
				response.Body.String() != tc.wantBody {
				t.Fatal("unexpected response body")
			}

			if tc.method == http.MethodHead &&
				response.Body.Len() != 0 {
				t.Fatal("HEAD response contains a body")
			}

			if tc.wantStatus == http.StatusNotFound &&
				strings.Contains(
					response.Body.String(),
					`id="root"`,
				) {
				t.Fatal(
					"missing asset returned SPA index",
				)
			}

			if tc.wantStatus ==
				http.StatusMethodNotAllowed &&
				response.Header().Get("Allow") != "GET, HEAD" {
				t.Fatal("incorrect Allow header")
			}
		})
	}
}

func TestConsoleCaching(t *testing.T) {
	handler, err := New(testAssets())
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		path        string
		contentType string
		cache       string
	}{
		{
			path:        "/",
			contentType: "text/html; charset=utf-8",
			cache:       "no-store",
		},
		{
			path:        "/assets/index-abcdefgh.js",
			contentType: "text/javascript; charset=utf-8",
			cache:       "private, max-age=31536000, immutable",
		},
		{
			path:        "/assets/index-abcdefgh.css",
			contentType: "text/css; charset=utf-8",
			cache:       "private, max-age=31536000, immutable",
		},
		{
			path:        "/assets/unhashed.js",
			contentType: "text/javascript; charset=utf-8",
			cache:       "no-store",
		},
	}

	for _, tc := range tests {
		t.Run(tc.path, func(t *testing.T) {
			request := httptest.NewRequest(
				http.MethodGet,
				tc.path,
				nil,
			)

			response := httptest.NewRecorder()

			handler.ServeHTTP(response, request)

			if response.Code != http.StatusOK {
				t.Fatalf(
					"unexpected status: %d",
					response.Code,
				)
			}

			if got := response.Header().Get(
				"Content-Type",
			); got != tc.contentType {
				t.Errorf(
					"Content-Type = %q, want %q",
					got,
					tc.contentType,
				)
			}

			if got := response.Header().Get(
				"Cache-Control",
			); got != tc.cache {
				t.Errorf(
					"Cache-Control = %q, want %q",
					got,
					tc.cache,
				)
			}
		})
	}
}

func TestConsoleSecurityHeaders(t *testing.T) {
	handler, err := New(testAssets())
	if err != nil {
		t.Fatal(err)
	}

	request := httptest.NewRequest(
		http.MethodGet,
		"/",
		nil,
	)

	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	headers := response.Header()

	expected := map[string]string{
		"X-Content-Type-Options":       "nosniff",
		"X-Frame-Options":              "DENY",
		"Referrer-Policy":              "no-referrer",
		"Cross-Origin-Resource-Policy": "same-origin",
		"Cache-Control":                "no-store",
	}

	for name, want := range expected {
		if got := headers.Get(name); got != want {
			t.Errorf(
				"%s = %q, want %q",
				name,
				got,
				want,
			)
		}
	}

	csp := headers.Get(
		"Content-Security-Policy",
	)

	for _, directive := range []string{
		"default-src 'none'",
		"script-src 'self'",
		"connect-src 'self'",
		"style-src-attr 'unsafe-inline'",
		"frame-ancestors 'none'",
		"object-src 'none'",
	} {
		if !strings.Contains(csp, directive) {
			t.Errorf(
				"missing CSP directive: %s",
				directive,
			)
		}
	}

	if strings.Contains(csp, "'unsafe-eval'") ||
		strings.Contains(
			csp,
			"script-src 'unsafe-inline'",
		) ||
		strings.Contains(
			csp,
			"script-src 'self' 'unsafe-inline'",
		) {
		t.Fatal("unsafe script execution permitted")
	}
}
