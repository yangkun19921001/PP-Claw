package tools

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWriteFileToolRejectsMissingContent(t *testing.T) {
	workspace := t.TempDir()
	tool := &WriteFileTool{Workspace: workspace, AllowedDir: workspace}

	_, err := tool.Execute(context.Background(), map[string]any{
		"path": "notes/output.txt",
	})
	if err == nil {
		t.Fatal("expected error for missing content")
	}
	if !strings.Contains(err.Error(), "content is required") {
		t.Fatalf("unexpected error: %v", err)
	}

	if _, statErr := os.Stat(filepath.Join(workspace, "notes", "output.txt")); !os.IsNotExist(statErr) {
		t.Fatalf("expected file to not exist, got stat err=%v", statErr)
	}
}

func TestWriteFileToolAllowsExplicitEmptyContent(t *testing.T) {
	workspace := t.TempDir()
	tool := &WriteFileTool{Workspace: workspace, AllowedDir: workspace}

	result, err := tool.Execute(context.Background(), map[string]any{
		"path":    "notes/empty.txt",
		"content": "",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, "0 bytes") {
		t.Fatalf("unexpected result: %s", result)
	}

	data, readErr := os.ReadFile(filepath.Join(workspace, "notes", "empty.txt"))
	if readErr != nil {
		t.Fatalf("read file: %v", readErr)
	}
	if len(data) != 0 {
		t.Fatalf("expected empty file, got %d bytes", len(data))
	}
}
