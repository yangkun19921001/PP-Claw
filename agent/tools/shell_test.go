package tools

import (
	"context"
	"strings"
	"testing"
)

func TestExecToolRejectsMissingCommand(t *testing.T) {
	tool := &ExecTool{WorkingDir: t.TempDir()}

	_, err := tool.Execute(context.Background(), map[string]any{})
	if err == nil {
		t.Fatal("expected error for missing command")
	}
	if !strings.Contains(err.Error(), "'command' parameter is required") {
		t.Fatalf("unexpected error: %v", err)
	}
}
