package config

import "testing"

func TestMatchProviderDoesNotFallbackOnExplicitUnconfiguredProvider(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Providers.OpenAI.APIKey = "openai-key"

	provider, name := cfg.matchProvider("anthropic/claude-sonnet-4-5")
	if provider != nil {
		t.Fatalf("expected nil provider when anthropic is unconfigured, got %#v", provider)
	}
	if name != "anthropic" {
		t.Fatalf("expected provider name anthropic, got %q", name)
	}
}

func TestMatchProviderUsesConfiguredProviderModelBeforeFallback(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Providers.OpenAI.APIKey = "openai-key"
	cfg.Providers.Anthropic.APIKey = "anthropic-key"
	cfg.Providers.Anthropic.Model = "claude-sonnet-4-5"

	provider, name := cfg.matchProvider("claude-sonnet-4-5")
	if provider == nil {
		t.Fatal("expected anthropic provider to be matched by providers.anthropic.model")
	}
	if name != "anthropic" {
		t.Fatalf("expected provider name anthropic, got %q", name)
	}
}

func TestMatchProviderFallsBackToConfiguredProviderWithoutKeywordGuessing(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Providers.OpenAI.APIKey = "openai-key"

	provider, name := cfg.matchProvider("claude-sonnet-4-5")
	if provider == nil {
		t.Fatal("expected configured provider fallback")
	}
	if name != "openai" {
		t.Fatalf("expected fallback provider openai, got %q", name)
	}
}

func TestMatchProviderSupportsOllamaWithoutAPIKey(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Providers.Ollama.BaseURL = "http://127.0.0.1:11434"

	provider, name := cfg.matchProvider("ollama/llama3.2")
	if provider == nil {
		t.Fatal("expected ollama provider to be considered configured without API key")
	}
	if name != "ollama" {
		t.Fatalf("expected provider name ollama, got %q", name)
	}
}

func TestMatchProviderSupportsCustomBaseURLWithoutAPIKey(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Providers.Custom.BaseURL = "http://127.0.0.1:8080/v1"

	provider, name := cfg.matchProvider("custom/my-local-model")
	if provider == nil {
		t.Fatal("expected custom provider to be considered configured when base_url is set")
	}
	if name != "custom" {
		t.Fatalf("expected provider name custom, got %q", name)
	}
}

func TestGetAPIBaseReturnsOllamaDefault(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Providers.Ollama.Model = "llama3.2"

	got := cfg.GetAPIBase("ollama/llama3.2")
	want := "http://127.0.0.1:11434"
	if got != want {
		t.Fatalf("expected ollama default api base %q, got %q", want, got)
	}
}
