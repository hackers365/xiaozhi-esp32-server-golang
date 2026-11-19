package middleware

import (
	"bytes"
	"strings"
	"testing"
)

func TestConfigureJWT(t *testing.T) {
	previous := currentSigningConfig.Load()
	t.Cleanup(func() { currentSigningConfig.Store(previous) })
	valid := strings.Repeat("k", 32)
	var previousGenerated []byte
	for _, tt := range []struct {
		name, input, want string
		invalid           bool
	}{
		{"empty", "", "", false},
		{"whitespace only", " \t", "", false},
		{"short", strings.Repeat("k", 31), "", true},
		{"legacy default", legacyJWTSecret, "", true},
		{"valid", valid, valid, false},
		{"trims whitespace", "  " + valid + "\t", valid, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			err := ConfigureJWT(tt.input, 24)
			if tt.invalid {
				if err == nil {
					t.Fatal("invalid JWT secret was accepted")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			key := currentSigningConfig.Load().secret
			if tt.want == "" {
				if len(key) != 32 || bytes.Equal(key, previousGenerated) {
					t.Fatal("expected a fresh 32-byte signing key")
				}
				previousGenerated = key
			} else if string(key) != tt.want {
				t.Fatal("configured signing key was not preserved")
			}
		})
	}

	token, err := GenerateToken(1, "user", "user")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ParseToken(token); err != nil {
		t.Fatalf("could not verify freshly issued token: %v", err)
	}
}
