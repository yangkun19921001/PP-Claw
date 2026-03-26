package providers

import (
	"testing"

	"github.com/yangkun19921001/PP-Claw/config"
)

func TestResolveActualModelStripsProviderPrefix(t *testing.T) {
	got := resolveActualModel("anthropic/claude-sonnet-4-5", "anthropic", &config.ProviderConfig{})
	want := "claude-sonnet-4-5"
	if got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
}

func TestResolveActualModelStripsGatewayPrefix(t *testing.T) {
	got := resolveActualModel("openrouter/openai/gpt-4.1", "openrouter", &config.ProviderConfig{})
	want := "openai/gpt-4.1"
	if got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
}

func TestResolveActualModelUsesProviderOverride(t *testing.T) {
	got := resolveActualModel("anthropic/claude-sonnet-4-5", "anthropic", &config.ProviderConfig{Model: "claude-opus-4-5"})
	want := "claude-opus-4-5"
	if got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
}
