package main

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/asynkron/climcp/internal/mcp"
)

func TestHumanBytes(t *testing.T) {
	tests := []struct {
		n    int
		want string
	}{
		{0, "0 B"},
		{512, "512 B"},
		{1024, "1.0 KB"},
		{51200, "50.0 KB"},
		{1 << 20, "1.0 MB"},
		{300000, "293.0 KB"},
	}
	for _, tt := range tests {
		if got := humanBytes(tt.n); got != tt.want {
			t.Errorf("humanBytes(%d) = %q, want %q", tt.n, got, tt.want)
		}
	}
}

func TestRenderResult(t *testing.T) {
	// Text content blocks are concatenated, one per line.
	r := &mcp.CallToolResult{Content: []mcp.ContentBlock{
		{Type: "text", Text: "hello"},
		{Type: "text", Text: "world"},
	}}
	if got := renderResult(r); got != "hello\nworld\n" {
		t.Errorf("renderResult text = %q", got)
	}

	// With no content blocks, structuredContent is used as a fallback.
	r2 := &mcp.CallToolResult{StructuredContent: json.RawMessage(`{"a":1}`)}
	if got := renderResult(r2); got != `{"a":1}`+"\n" {
		t.Errorf("renderResult structured = %q", got)
	}
}

func TestTooLargeErrorBoundsPreview(t *testing.T) {
	// tooLargeError must never let more than previewBytes reach the error path's
	// stdout; here we just assert the returned message mentions the sizes.
	big := strings.Repeat("x", 300000)
	err := tooLargeError(big, defaultMaxBytes)
	if err == nil {
		t.Fatal("expected an error for oversized output")
	}
	for _, want := range []string{"too large", "293.0 KB", "50.0 KB"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q missing %q", err.Error(), want)
		}
	}
}
