package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestTransportAndEndpoint(t *testing.T) {
	tests := []struct {
		name      string
		srv       Server
		transport string
		endpoint  string
	}{
		{"stdio implicit", Server{Command: "node", Args: []string{"s.js"}}, "stdio", "node s.js"},
		{"http via url", Server{URL: "https://x/mcp"}, "http", "https://x/mcp"},
		{"http via type", Server{Type: "http", URL: "https://y"}, "http", "https://y"},
		{"sse maps to http", Server{Type: "sse", URL: "https://z"}, "http", "https://z"},
		{"explicit stdio", Server{Type: "stdio", Command: "x"}, "stdio", "x"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.srv.Transport(); got != tt.transport {
				t.Errorf("Transport() = %q, want %q", got, tt.transport)
			}
			if got := tt.srv.Endpoint(); got != tt.endpoint {
				t.Errorf("Endpoint() = %q, want %q", got, tt.endpoint)
			}
		})
	}
}

func TestSuggest(t *testing.T) {
	c := &Config{Servers: map[string]Server{
		"filesystem": {}, "github": {}, "memory": {},
	}}
	tests := []struct {
		input string
		want  string
	}{
		{"filesystm", "filesystem"}, // one deletion
		{"gihub", "github"},         // transposition-ish
		{"mem", "memory"},           // prefix, within threshold
		{"xq", ""},                  // nothing close
		{"zzzzzzzz", ""},            // nothing close
	}
	for _, tt := range tests {
		if got := c.Suggest(tt.input); got != tt.want {
			t.Errorf("Suggest(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestGetUnknownSuggests(t *testing.T) {
	c := &Config{Path: "x.json", Servers: map[string]Server{"mock": {}}}
	_, err := c.Get("moc")
	if err == nil {
		t.Fatal("expected error for unknown server")
	}
	if !contains(err.Error(), `Did you mean "mock"`) {
		t.Errorf("error should suggest a name, got: %v", err)
	}
}

func TestLoad(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "climcp.json")
	body := `{"mcpServers":{"a":{"command":"x"},"b":{"url":"https://y"}}}`
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(cfg.Servers) != 2 {
		t.Fatalf("got %d servers, want 2", len(cfg.Servers))
	}
	names := cfg.Names()
	if len(names) != 2 || names[0] != "a" || names[1] != "b" {
		t.Errorf("Names() = %v, want sorted [a b]", names)
	}
	if _, err := Load(filepath.Join(dir, "missing.json")); err == nil {
		t.Error("expected error loading a missing explicit path")
	}
}

func TestParseServers(t *testing.T) {
	dir := t.TempDir()
	write := func(name, body string) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		return p
	}

	mcp := write("mcp.json", `{"mcpServers":{"a":{"command":"x"}}}`)
	vscode := write("vscode.json", `{"servers":{"b":{"url":"https://y"}}}`)
	empty := write("empty.json", `{"other":true}`)

	if got, err := ParseServers(mcp); err != nil || len(got) != 1 || got["a"].Command != "x" {
		t.Errorf("mcpServers shape: got %#v err %v", got, err)
	}
	if got, err := ParseServers(vscode); err != nil || len(got) != 1 || got["b"].URL != "https://y" {
		t.Errorf("servers shape: got %#v err %v", got, err)
	}
	if got, err := ParseServers(empty); err != nil || len(got) != 0 {
		t.Errorf("no-servers file should yield empty map, got %#v err %v", got, err)
	}
	if _, err := ParseServers(filepath.Join(dir, "missing.json")); err == nil {
		t.Error("expected error for missing file")
	}
}

func TestSaveRoundTrip(t *testing.T) {
	// Save into a nested path that doesn't exist yet, then load it back.
	path := filepath.Join(t.TempDir(), "nested", "dir", "climcp.json")
	servers := map[string]Server{
		"a": {Command: "x", Args: []string{"--flag"}},
		"b": {Type: "http", URL: "https://y"},
	}
	if err := Save(path, servers); err != nil {
		t.Fatalf("Save: %v", err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(cfg.Servers) != 2 || cfg.Servers["a"].Command != "x" || cfg.Servers["b"].URL != "https://y" {
		t.Errorf("round-trip mismatch: %#v", cfg.Servers)
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || indexOf(s, sub) >= 0)
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
