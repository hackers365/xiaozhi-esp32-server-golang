package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadWithPathJWTSecret(t *testing.T) {
	path := filepath.Join(t.TempDir(), "manager.json")
	fileSecret := strings.Repeat("f", 32)
	if err := os.WriteFile(path, []byte(`{"jwt":{"secret":"`+fileSecret+`"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	envSecret := strings.Repeat("e", 32)
	for _, tt := range []struct {
		name, env, want string
	}{
		{"override", "  " + envSecret + "\t", envSecret},
		{"blank does not override", " \t", fileSecret},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("JWT_SECRET", tt.env)
			if got := LoadWithPath(path).JWT.Secret; got != tt.want {
				t.Error("unexpected JWT secret source")
			}
		})
	}
}
