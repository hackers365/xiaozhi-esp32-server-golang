package dockercompose_test

import (
	"bytes"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/spf13/viper"
	"gopkg.in/yaml.v3"
)

type composeFile struct {
	Services map[string]composeService `yaml:"services"`
}

type composeService struct {
	Image       string   `yaml:"image"`
	Ports       []string `yaml:"ports"`
	Environment any      `yaml:"environment"`
}

func parseEnv(raw any) map[string]string {
	env := make(map[string]string)
	switch typed := raw.(type) {
	case []any:
		for _, item := range typed {
			s, ok := item.(string)
			if !ok {
				continue
			}
			k, v, hasEq := strings.Cut(s, "=")
			if !hasEq {
				continue
			}
			env[strings.TrimSpace(k)] = strings.TrimSpace(v)
		}
	case map[string]any:
		for k, v := range typed {
			if s, ok := v.(string); ok {
				env[strings.TrimSpace(k)] = strings.TrimSpace(s)
			}
		}
	}
	return env
}

// TestDockerComposeVoiceServerConfig verifies that docker/docker-composer/docker-compose.yml
// configures ghcr.io/hackers365/voice_server:0.1.2 with the exact environment variables
// required by voice_server v0.1.2 (which changed default server.port to 9000 and renamed
// speaker.vector_db to speaker.qdrant with a hardcoded LAN host in config.json).
func TestDockerComposeVoiceServerConfig(t *testing.T) {
	composePath := filepath.Join("..", "..", "docker", "docker-composer", "docker-compose.yml")
	raw, err := os.ReadFile(composePath)
	if err != nil {
		t.Fatalf("failed to read %s: %v", composePath, err)
	}

	var cfg composeFile
	if err := yaml.Unmarshal(raw, &cfg); err != nil {
		t.Fatalf("failed to parse %s: %v", composePath, err)
	}

	voiceSvc, ok := cfg.Services["voice-server"]
	if !ok {
		t.Fatalf("voice-server service not found in %s", composePath)
	}
	backendSvc, ok := cfg.Services["backend"]
	if !ok {
		t.Fatalf("backend service not found in %s", composePath)
	}

	voiceEnv := parseEnv(voiceSvc.Environment)
	backendEnv := parseEnv(backendSvc.Environment)

	// Old v0.1.0 env keys are ignored by voice_server v0.1.2 and should not be relied upon.
	for _, legacyKey := range []string{
		"VAD_ASR_SPEAKER_VECTOR_DB_HOST",
		"VAD_ASR_SPEAKER_VECTOR_DB_PORT",
		"VAD_ASR_SPEAKER_VECTOR_DB_COLLECTION_NAME",
	} {
		if _, exists := voiceEnv[legacyKey]; exists {
			t.Errorf("voice-server should not use removed v0.1.0 env key %s", legacyKey)
		}
	}

	// Simulate voice_server v0.1.2 config loading:
	// config.json defaults to server.port=9000 and speaker.qdrant.host=192.168.208.214.
	for k, v := range voiceEnv {
		t.Setenv(k, v)
	}

	v := viper.New()
	v.SetConfigType("json")
	v.SetEnvPrefix("VAD_ASR")
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.AutomaticEnv()

	const voiceServerV012DefaultConfigJSON = `{
		"server": { "port": 9000, "host": "0.0.0.0" },
		"speaker": {
			"enabled": true,
			"threshold": 0.6,
			"storage_type": "qdrant",
			"qdrant": {
				"host": "192.168.208.214",
				"port": 6334,
				"collection_name": "speaker_embeddings"
			}
		}
	}`
	if err := v.ReadConfig(bytes.NewBufferString(voiceServerV012DefaultConfigJSON)); err != nil {
		t.Fatalf("failed to read simulated voice_server config: %v", err)
	}

	type resolvedVoiceServerConfig struct {
		Server struct {
			Port int `mapstructure:"port"`
		} `mapstructure:"server"`
		Speaker struct {
			Enabled     bool    `mapstructure:"enabled"`
			Threshold   float32 `mapstructure:"threshold"`
			StorageType string  `mapstructure:"storage_type"`
			Qdrant      struct {
				Host           string `mapstructure:"host"`
				Port           int    `mapstructure:"port"`
				CollectionName string `mapstructure:"collection_name"`
			} `mapstructure:"qdrant"`
		} `mapstructure:"speaker"`
	}

	var resolved resolvedVoiceServerConfig
	if err := v.Unmarshal(&resolved); err != nil {
		t.Fatalf("failed to unmarshal simulated voice_server config: %v", err)
	}

	// Mirror voice_server internal/bootstrap/init.go QDRANT_* env overrides.
	qdrantHost := resolved.Speaker.Qdrant.Host
	if envHost := os.Getenv("QDRANT_HOST"); envHost != "" {
		qdrantHost = envHost
	}
	qdrantPort := resolved.Speaker.Qdrant.Port
	if envPort := os.Getenv("QDRANT_PORT"); envPort != "" {
		if parsed, err := strconv.Atoi(envPort); err == nil {
			qdrantPort = parsed
		}
	}
	qdrantCollection := resolved.Speaker.Qdrant.CollectionName
	if envCollection := os.Getenv("QDRANT_COLLECTION_NAME"); envCollection != "" {
		qdrantCollection = envCollection
	}

	// Verify backend SPEAKER_SERVICE_URL port matches voice-server container port and port mapping.
	speakerURL, err := url.Parse(backendEnv["SPEAKER_SERVICE_URL"])
	if err != nil {
		t.Fatalf("invalid SPEAKER_SERVICE_URL %q: %v", backendEnv["SPEAKER_SERVICE_URL"], err)
	}
	expectedPort, err := strconv.Atoi(speakerURL.Port())
	if err != nil {
		t.Fatalf("missing port in SPEAKER_SERVICE_URL %q: %v", backendEnv["SPEAKER_SERVICE_URL"], err)
	}

	if resolved.Server.Port != expectedPort {
		t.Errorf("resolved voice-server port = %d, want %d (matching SPEAKER_SERVICE_URL=%s)",
			resolved.Server.Port, expectedPort, backendEnv["SPEAKER_SERVICE_URL"])
	}

	portMappingMatched := false
	for _, p := range voiceSvc.Ports {
		if strings.HasSuffix(p, ":"+strconv.Itoa(resolved.Server.Port)) {
			portMappingMatched = true
			break
		}
	}
	if !portMappingMatched {
		t.Errorf("voice-server ports %v do not map container port %d", voiceSvc.Ports, resolved.Server.Port)
	}

	if !resolved.Speaker.Enabled {
		t.Errorf("expected speaker.enabled=true")
	}
	if resolved.Speaker.StorageType != "qdrant" {
		t.Errorf("resolved speaker.storage_type = %q, want %q", resolved.Speaker.StorageType, "qdrant")
	}
	if resolved.Speaker.Qdrant.Host != "qdrant" {
		t.Errorf("resolved viper speaker.qdrant.host = %q, want %q", resolved.Speaker.Qdrant.Host, "qdrant")
	}
	if qdrantHost != "qdrant" {
		t.Errorf("resolved Qdrant host = %q, want %q", qdrantHost, "qdrant")
	}
	if qdrantPort != 6334 {
		t.Errorf("resolved Qdrant port = %d, want 6334", qdrantPort)
	}
	if qdrantCollection != "speaker_embeddings" {
		t.Errorf("resolved Qdrant collection = %q, want %q", qdrantCollection, "speaker_embeddings")
	}
}
