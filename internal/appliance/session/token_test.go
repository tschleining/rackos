package session

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
)

func TestGenerateToken(t *testing.T) {
	seen := make(map[string]bool)

	for i := 0; i < 100; i++ {
		token, digest, err := generateToken()
		if err != nil {
			t.Fatal(err)
		}

		if len(token) != EncodedTokenSize {
			t.Fatal("incorrect encoded token length")
		}

		raw, err := base64.RawURLEncoding.
			Strict().
			DecodeString(token)

		if err != nil || len(raw) != TokenSize {
			t.Fatal("invalid generated token")
		}

		expected := sha256.Sum256(raw)

		if expected != digest {
			t.Fatal("incorrect generated token digest")
		}

		lookupDigest, err := hashToken(token)
		if err != nil {
			t.Fatal(err)
		}

		if lookupDigest != digest {
			t.Fatal("lookup digest does not match")
		}

		if seen[token] {
			t.Fatal("duplicate generated token")
		}

		seen[token] = true
	}
}

func TestHashTokenKnownInput(t *testing.T) {
	var raw [TokenSize]byte

	for i := range raw {
		raw[i] = byte(i)
	}

	token := base64.RawURLEncoding.
		EncodeToString(raw[:])

	expected := sha256.Sum256(raw[:])

	actual, err := hashToken(token)
	if err != nil {
		t.Fatal(err)
	}

	if actual != expected {
		t.Fatal("incorrect token digest")
	}

	// Hashing the textual Base64URL representation
	// is not our storage format.
	encodedDigest := sha256.Sum256([]byte(token))

	if actual == encodedDigest {
		t.Fatal("digest was derived from encoded text")
	}
}

func TestHashTokenRejectsInvalidEncoding(
	t *testing.T,
) {
	var raw [TokenSize]byte

	canonical := base64.RawURLEncoding.
		EncodeToString(raw[:])

	tests := []struct {
		name  string
		token string
	}{
		{"empty", ""},
		{"short", canonical[:42]},
		{"long", canonical + "A"},
		{"padded", canonical + "="},
		{"leading whitespace", " " + canonical},
		{"trailing whitespace", canonical + " "},
		{"embedded whitespace", canonical[:42] + " "},
		{"embedded newline", canonical[:42] + "\n"},
		{"embedded NUL", canonical[:42] + "\x00"},
		{"invalid character", canonical[:42] + "!"},
		{"standard base64 plus", "+" + canonical[1:]},
		{"standard base64 slash", "/" + canonical[1:]},
		{
			"noncanonical trailing bits",
			canonical[:42] + "B",
		},
		{
			"oversized",
			strings.Repeat("A", 4096),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			digest, err := hashToken(tc.token)

			if !errors.Is(err, ErrInvalidToken) {
				t.Fatal("invalid token was accepted")
			}

			if digest != ([TokenHashSize]byte{}) {
				t.Fatal("invalid token returned a digest")
			}
		})
	}
}

func TestGenerateSessionID(t *testing.T) {
	seen := make(map[string]bool)

	for i := 0; i < 100; i++ {
		id, err := generateSessionID()
		if err != nil {
			t.Fatal(err)
		}

		if len(id) != SessionIDSize*2 {
			t.Fatal("incorrect session identity length")
		}

		raw, err := hex.DecodeString(id)
		if err != nil || len(raw) != SessionIDSize {
			t.Fatal("invalid session identity")
		}

		if hex.EncodeToString(raw) != id {
			t.Fatal("noncanonical session identity")
		}

		if seen[id] {
			t.Fatal("duplicate session identity")
		}

		seen[id] = true
	}
}

func TestIssuedSessionRedaction(t *testing.T) {
	token, _, err := generateToken()
	if err != nil {
		t.Fatal(err)
	}

	issued := IssuedSession{
		Session: Session{
			ID: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		},
		token: token,
	}

	if issued.Token() != token {
		t.Fatal("token accessor returned wrong value")
	}

	encoded, err := json.Marshal(issued)
	if err != nil {
		t.Fatal(err)
	}

	outputs := []string{
		fmt.Sprintf("%v", issued),
		fmt.Sprintf("%+v", issued),
		fmt.Sprintf("%#v", issued),
		fmt.Sprintf("%s", issued),
		string(encoded),
	}

	for _, output := range outputs {
		if strings.Contains(output, token) {
			t.Fatal("authentication token was disclosed")
		}
	}

	if !strings.Contains(
		string(encoded),
		issued.Session.ID,
	) {
		t.Fatal("nonsecret session metadata missing")
	}
}
