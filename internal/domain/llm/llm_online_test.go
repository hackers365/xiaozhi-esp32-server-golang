package llm

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/cloudwego/eino/schema"
)

func onlineEnabledLLM(t *testing.T) {
	t.Helper()
	if os.Getenv("RUN_ONLINE_TESTS") != "1" {
		t.Skip("skip online LLM tests: set RUN_ONLINE_TESTS=1")
	}
}

func requireEnvLLM(t *testing.T, key string) string {
	t.Helper()
	v := os.Getenv(key)
	if v == "" {
		t.Skipf("skip online LLM test: missing %s", key)
	}
	return v
}

func runLLMOnlineCase(t *testing.T, provider string, cfg map[string]interface{}) {
	t.Helper()
	llmProvider, err := GetLLMProvider(provider, cfg)
	if err != nil {
		t.Fatalf("create LLM provider failed: %v", err)
	}
	defer llmProvider.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()

	messages := []*schema.Message{{Role: schema.User, Content: "请回复:ok"}}
	ch := llmProvider.ResponseWithContext(ctx, "online-test", messages, nil)

	select {
	case msg, ok := <-ch:
		if !ok {
			t.Fatalf("response channel closed unexpectedly")
		}
		if msg == nil {
			t.Fatalf("received nil response")
		}
	case <-ctx.Done():
		t.Fatalf("LLM response timeout: %v", ctx.Err())
	}
}

func TestLLMOnlineProviders(t *testing.T) {
	onlineEnabledLLM(t)

	t.Run("eino_openai", func(t *testing.T) {
		apiKey := firstNonEmpty(os.Getenv("LLM_OPENAI_API_KEY"), os.Getenv("OPENAI_API_KEY"))
		if apiKey == "" {
			t.Skip("skip online LLM test: missing LLM_OPENAI_API_KEY or OPENAI_API_KEY")
		}
		runLLMOnlineCase(t, "openai", map[string]interface{}{
			"type":       "openai",
			"model_name": getenvOrDefault("LLM_OPENAI_MODEL", "gpt-4o-mini"),
			"api_key":    apiKey,
			"streamable": true,
		})
	})

	t.Run("dify", func(t *testing.T) {
		runLLMOnlineCase(t, "dify", map[string]interface{}{
			"type":     "dify",
			"api_key":  requireEnvLLM(t, "LLM_DIFY_API_KEY"),
			"base_url": os.Getenv("LLM_DIFY_BASE_URL"),
		})
	})

	t.Run("coze", func(t *testing.T) {
		runLLMOnlineCase(t, "coze", map[string]interface{}{
			"type":     "coze",
			"api_key":  requireEnvLLM(t, "LLM_COZE_API_KEY"),
			"bot_id":   requireEnvLLM(t, "LLM_COZE_BOT_ID"),
			"base_url": os.Getenv("LLM_COZE_BASE_URL"),
		})
	})
}

func getenvOrDefault(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}
