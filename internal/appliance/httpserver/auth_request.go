package httpserver

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/tschleining/rackos/internal/appliance/admin"
	"github.com/tschleining/rackos/internal/appliance/auth"
)

// Allow the maximum supported password to be
// represented using JSON Unicode escapes while
// maintaining a bounded HTTP request body.
const maxLoginRequestBytes = 6*auth.MaxPasswordInputBytes + 1024

var (
	errInvalidLoginRequest = errors.New(
		"invalid login request",
	)

	errLoginRequestTooLarge = errors.New(
		"login request too large",
	)

	errUnsupportedLoginMediaType = errors.New(
		"unsupported login media type",
	)
)

type authLoginRequest struct {
	username string
	password []byte
}

// String prevents ordinary formatted output from
// accidentally exposing authentication credentials.
func (r authLoginRequest) String() string {
	return "[redacted login request]"
}

func (r authLoginRequest) GoString() string {
	return "[redacted login request]"
}

func (r *authLoginRequest) clearPassword() {
	clear(r.password)
	r.password = nil
}

func decodeLoginRequest(
	w http.ResponseWriter,
	r *http.Request,
) (authLoginRequest, error) {
	if r.URL.RawQuery != "" || r.URL.ForceQuery {
		return authLoginRequest{},
			errInvalidLoginRequest
	}

	contentTypes := r.Header.Values("Content-Type")

	if len(contentTypes) != 1 {
		return authLoginRequest{},
			errUnsupportedLoginMediaType
	}

	mediaType, params, err := mime.ParseMediaType(
		contentTypes[0],
	)

	if err != nil || mediaType != "application/json" {
		return authLoginRequest{},
			errUnsupportedLoginMediaType
	}

	for name, value := range params {
		if name != "charset" ||
			!strings.EqualFold(value, "utf-8") {
			return authLoginRequest{},
				errUnsupportedLoginMediaType
		}
	}

	// Authentication requests do not support
	// compressed or transformed request bodies.
	if len(r.Header.Values("Content-Encoding")) != 0 {
		return authLoginRequest{},
			errUnsupportedLoginMediaType
	}

	if r.Body == nil {
		return authLoginRequest{},
			errInvalidLoginRequest
	}

	body := http.MaxBytesReader(
		w,
		r.Body,
		int64(maxLoginRequestBytes),
	)
	defer body.Close()

	payload, err := io.ReadAll(body)
	defer clear(payload)

	if err != nil {
		var sizeErr *http.MaxBytesError

		if errors.As(err, &sizeErr) {
			return authLoginRequest{},
				errLoginRequestTooLarge
		}

		return authLoginRequest{},
			errInvalidLoginRequest
	}

	if !utf8.Valid(payload) {
		return authLoginRequest{},
			errInvalidLoginRequest
	}

	return parseLoginRequest(payload)
}

func parseLoginRequest(
	payload []byte,
) (authLoginRequest, error) {
	var result authLoginRequest
	success := false

	defer func() {
		if !success {
			result.clearPassword()
		}
	}()

	decoder := json.NewDecoder(
		bytes.NewReader(payload),
	)

	start, err := decoder.Token()

	if err != nil || start != json.Delim('{') {
		return authLoginRequest{},
			errInvalidLoginRequest
	}

	var seenUsername bool
	var seenPassword bool

	for decoder.More() {
		key, err := decoder.Token()
		if err != nil {
			return authLoginRequest{},
				errInvalidLoginRequest
		}

		name, ok := key.(string)
		if !ok {
			return authLoginRequest{},
				errInvalidLoginRequest
		}

		switch name {
		case "username":
			if seenUsername {
				return authLoginRequest{},
					errInvalidLoginRequest
			}

			seenUsername = true

			value, err := decodeLoginString(decoder)
			if err != nil {
				return authLoginRequest{}, err
			}

			result.username, err =
				admin.NormalizeUsername(value)

			if err != nil {
				return authLoginRequest{},
					errInvalidLoginRequest
			}

		case "password":
			if seenPassword {
				return authLoginRequest{},
					errInvalidLoginRequest
			}

			seenPassword = true

			value, err := decodeLoginString(decoder)
			if err != nil {
				return authLoginRequest{}, err
			}

			if len(value) == 0 ||
				len(value) > auth.MaxPasswordInputBytes {
				return authLoginRequest{},
					errInvalidLoginRequest
			}

			result.password = []byte(value)

		default:
			return authLoginRequest{},
				errInvalidLoginRequest
		}
	}

	end, err := decoder.Token()

	if err != nil || end != json.Delim('}') {
		return authLoginRequest{},
			errInvalidLoginRequest
	}

	// Reject additional JSON documents and
	// non-whitespace trailing content.
	if _, err := decoder.Token(); err != io.EOF {
		return authLoginRequest{},
			errInvalidLoginRequest
	}

	if !seenUsername || !seenPassword {
		return authLoginRequest{},
			errInvalidLoginRequest
	}

	success = true

	return result, nil
}

// decodeLoginString rejects null and non-string
// JSON values, including malformed Unicode escapes.
func decodeLoginString(
	decoder *json.Decoder,
) (string, error) {
	var raw json.RawMessage

	if err := decoder.Decode(&raw); err != nil {
		return "", errInvalidLoginRequest
	}
	defer clear(raw)

	if len(raw) < 2 || raw[0] != '"' ||
		raw[len(raw)-1] != '"' {
		return "", errInvalidLoginRequest
	}

	if !validLoginStringUnicode(raw) {
		return "", errInvalidLoginRequest
	}

	var value string

	if err := json.Unmarshal(raw, &value); err != nil {
		return "", errInvalidLoginRequest
	}

	return value, nil
}

// Go's standard JSON decoder replaces unpaired
// UTF-16 surrogate escapes with U+FFFD.
//
// Authentication requests must reject these
// malformed representations instead of silently
// altering supplied credentials.
func validLoginStringUnicode(raw []byte) bool {
	for i := 1; i < len(raw)-1; i++ {
		if raw[i] != '\\' {
			continue
		}

		i++

		if i >= len(raw)-1 {
			return false
		}

		if raw[i] != 'u' {
			continue
		}

		if i+4 >= len(raw)-1 {
			return false
		}

		value, err := strconv.ParseUint(
			string(raw[i+1:i+5]),
			16,
			16,
		)
		if err != nil {
			return false
		}

		i += 4

		switch {
		case value >= 0xD800 && value <= 0xDBFF:
			// A high surrogate must be followed
			// immediately by a low surrogate.
			if i+6 >= len(raw)-1 ||
				raw[i+1] != '\\' ||
				raw[i+2] != 'u' {
				return false
			}

			low, err := strconv.ParseUint(
				string(raw[i+3:i+7]),
				16,
				16,
			)

			if err != nil ||
				low < 0xDC00 ||
				low > 0xDFFF {
				return false
			}

			i += 6

		case value >= 0xDC00 && value <= 0xDFFF:
			return false
		}
	}

	return true
}
