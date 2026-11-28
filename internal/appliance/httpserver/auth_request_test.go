package httpserver

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/tschleining/rackos/internal/appliance/auth"
)

func TestDecodeLoginRequestValid(t *testing.T) {
	tests := []struct {
		name     string
		body     string
		password string
	}{
		{
			name:     "ordinary credentials",
			body:     `{"username":"ADMIN","password":"example-password"}`,
			password: "example-password",
		},
		{
			name:     "reversed fields",
			body:     `{"password":"example-password","username":"admin"}`,
			password: "example-password",
		},
		{
			name:     "preserve password whitespace",
			body:     `{"username":"admin","password":" example password "}`,
			password: " example password ",
		},
		{
			name:     "Unicode password",
			body:     `{"username":"admin","password":"café"}`,
			password: "café",
		},
		{
			name:     "Unicode surrogate pair",
			body:     `{"username":"admin","password":"\uD83D\uDE00"}`,
			password: "😀",
		},
		{
			name:     "escaped backslash",
			body:     `{"username":"admin","password":"\\uD800"}`,
			password: `\uD800`,
		},
		{
			name:     "replacement character explicitly supplied",
			body:     `{"username":"admin","password":"\uFFFD"}`,
			password: "\uFFFD",
		},
		{
			name:     "trailing whitespace",
			body:     "{\"username\":\"admin\",\"password\":\"test\"}\n\t ",
			password: "test",
		},
		{
			name: "maximum password length",
			body: `{"username":"admin","password":"` +
				strings.Repeat("a", auth.MaxPasswordInputBytes) +
				`"}`,
			password: strings.Repeat(
				"a",
				auth.MaxPasswordInputBytes,
			),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			request := httptest.NewRequest(
				http.MethodPost,
				"https://127.0.0.1/api/v1/auth/login",
				strings.NewReader(tt.body),
			)

			request.Header.Set(
				"Content-Type",
				"application/json; charset=utf-8",
			)

			result, err := decodeLoginRequest(
				httptest.NewRecorder(),
				request,
			)
			if err != nil {
				t.Fatal(err)
			}
			defer result.clearPassword()

			if result.username != "admin" {
				t.Fatal("username was not normalized")
			}

			if string(result.password) != tt.password {
				t.Fatal("password was altered")
			}
		})
	}
}

func TestDecodeLoginRequestInvalid(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{"empty body", ""},
		{"empty object", `{}`},
		{"null body", `null`},
		{"array body", `[]`},
		{"string body", `"credentials"`},
		{
			"missing username",
			`{"password":"test"}`,
		},
		{
			"missing password",
			`{"username":"admin"}`,
		},
		{
			"empty username",
			`{"username":"","password":"test"}`,
		},
		{
			"invalid username",
			`{"username":" admin ","password":"test"}`,
		},
		{
			"empty password",
			`{"username":"admin","password":""}`,
		},
		{
			"null username",
			`{"username":null,"password":"test"}`,
		},
		{
			"null password",
			`{"username":"admin","password":null}`,
		},
		{
			"numeric password",
			`{"username":"admin","password":123}`,
		},
		{
			"object password",
			`{"username":"admin","password":{}}`,
		},
		{
			"unknown field",
			`{"username":"admin","password":"test","extra":true}`,
		},
		{
			"duplicate username",
			`{"username":"admin","username":"admin","password":"test"}`,
		},
		{
			"duplicate password",
			`{"username":"admin","password":"first","password":"second"}`,
		},
		{
			"incorrect field capitalization",
			`{"Username":"admin","password":"test"}`,
		},
		{
			"escaped duplicate key",
			`{"username":"admin","password":"test","\u0070assword":"other"}`,
		},
		{
			"trailing document",
			`{"username":"admin","password":"test"}{}`,
		},
		{
			"trailing garbage",
			`{"username":"admin","password":"test"}garbage`,
		},
		{
			"truncated document",
			`{"username":"admin","password":"test"`,
		},
		{
			"unpaired high surrogate",
			`{"username":"admin","password":"\uD800"}`,
		},
		{
			"unpaired low surrogate",
			`{"username":"admin","password":"\uDC00"}`,
		},
		{
			"invalid surrogate pair",
			`{"username":"admin","password":"\uD800\u0041"}`,
		},
		{
			"invalid UTF-8",
			"{\"username\":\"admin\",\"password\":\"\xff\"}",
		},
		{
			"password too long",
			`{"username":"admin","password":"` +
				strings.Repeat(
					"a",
					auth.MaxPasswordInputBytes+1,
				) + `"}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			request := httptest.NewRequest(
				http.MethodPost,
				"https://127.0.0.1/api/v1/auth/login",
				strings.NewReader(tt.body),
			)

			request.Header.Set(
				"Content-Type",
				"application/json",
			)

			result, err := decodeLoginRequest(
				httptest.NewRecorder(),
				request,
			)

			if !errors.Is(err, errInvalidLoginRequest) {
				t.Fatalf("unexpected error: %v", err)
			}

			if result.username != "" ||
				len(result.password) != 0 {
				t.Fatal("invalid request returned credentials")
			}
		})
	}
}

func TestDecodeLoginRequestMediaType(t *testing.T) {
	tests := []struct {
		name     string
		values   []string
		expected error
	}{
		{
			name:   "JSON",
			values: []string{"application/json"},
		},
		{
			name:   "UTF-8 JSON",
			values: []string{"application/json; charset=UTF-8"},
		},
		{
			name:     "missing content type",
			expected: errUnsupportedLoginMediaType,
		},
		{
			name:     "plain text",
			values:   []string{"text/plain"},
			expected: errUnsupportedLoginMediaType,
		},
		{
			name: "form submission",
			values: []string{
				"application/x-www-form-urlencoded",
			},
			expected: errUnsupportedLoginMediaType,
		},
		{
			name: "unsupported charset",
			values: []string{
				"application/json; charset=iso-8859-1",
			},
			expected: errUnsupportedLoginMediaType,
		},
		{
			name: "unknown parameter",
			values: []string{
				"application/json; unknown=value",
			},
			expected: errUnsupportedLoginMediaType,
		},
		{
			name: "duplicate content type",
			values: []string{
				"application/json",
				"application/json",
			},
			expected: errUnsupportedLoginMediaType,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			request := httptest.NewRequest(
				http.MethodPost,
				"https://127.0.0.1/api/v1/auth/login",
				strings.NewReader(
					`{"username":"admin","password":"test"}`,
				),
			)

			for _, value := range tt.values {
				request.Header.Add(
					"Content-Type",
					value,
				)
			}

			result, err := decodeLoginRequest(
				httptest.NewRecorder(),
				request,
			)
			defer result.clearPassword()

			if !errors.Is(err, tt.expected) {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestDecodeLoginRequestBodyLimit(t *testing.T) {
	request := httptest.NewRequest(
		http.MethodPost,
		"https://127.0.0.1/api/v1/auth/login",
		strings.NewReader(
			strings.Repeat(
				" ",
				int(maxLoginRequestBytes)+1,
			),
		),
	)

	request.Header.Set(
		"Content-Type",
		"application/json",
	)

	_, err := decodeLoginRequest(
		httptest.NewRecorder(),
		request,
	)

	if !errors.Is(err, errLoginRequestTooLarge) {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestDecodeLoginRequestQueryRejected(t *testing.T) {
	for _, query := range []string{
		"?",
		"?username=admin",
		"?password=test",
	} {
		request := httptest.NewRequest(
			http.MethodPost,
			"https://127.0.0.1/api/v1/auth/login"+query,
			strings.NewReader(
				`{"username":"admin","password":"test"}`,
			),
		)

		request.Header.Set(
			"Content-Type",
			"application/json",
		)

		_, err := decodeLoginRequest(
			httptest.NewRecorder(),
			request,
		)

		if !errors.Is(err, errInvalidLoginRequest) {
			t.Fatal("query parameters were accepted")
		}
	}
}

func TestDecodeLoginRequestContentEncodingRejected(
	t *testing.T,
) {
	request := httptest.NewRequest(
		http.MethodPost,
		"https://127.0.0.1/api/v1/auth/login",
		strings.NewReader(
			`{"username":"admin","password":"test"}`,
		),
	)

	request.Header.Set(
		"Content-Type",
		"application/json",
	)

	request.Header.Set("Content-Encoding", "gzip")

	_, err := decodeLoginRequest(
		httptest.NewRecorder(),
		request,
	)

	if !errors.Is(err, errUnsupportedLoginMediaType) {
		t.Fatal("encoded request body was accepted")
	}
}

func TestAuthLoginRequestRedaction(t *testing.T) {
	request := authLoginRequest{
		username: "admin",
		password: []byte("sensitive-test-password"),
	}
	defer request.clearPassword()

	for _, format := range []string{
		"%v",
		"%+v",
		"%#v",
		"%s",
	} {
		output := fmt.Sprintf(format, request)

		if strings.Contains(
			output,
			"sensitive-test-password",
		) {
			t.Fatal("formatted request exposed password")
		}
	}
}

func TestAuthLoginRequestClearPassword(t *testing.T) {
	password := []byte("test-password")

	request := authLoginRequest{
		password: password,
	}

	request.clearPassword()

	if request.password != nil {
		t.Fatal("password reference was retained")
	}

	for _, value := range password {
		if value != 0 {
			t.Fatal("password buffer was not cleared")
		}
	}
}
