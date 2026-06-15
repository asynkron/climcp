// Package config loads the climcp server configuration. The file format is
// intentionally compatible with the mcp.json shape that agents already use:
//
//	{
//	  "mcpServers": {
//	    "filesystem": {
//	      "command": "npx",
//	      "args": ["-y", "@modelcontextprotocol/server-filesystem", "/tmp"],
//	      "env": { "FOO": "bar" }
//	    }
//	  }
//	}
package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Server describes a single configured MCP server. It is either a stdio server
// (a spawned process, via Command/Args/Env/Cwd) or a remote server reached over
// HTTP (via URL/Headers). The transport is inferred: a URL — or an explicit
// Type of "http"/"sse"/"streamable-http" — selects HTTP; otherwise stdio.
type Server struct {
	// Type optionally pins the transport: "stdio", "http", "sse", or
	// "streamable-http". When empty it is inferred from the other fields.
	Type string `json:"type,omitempty"`

	// stdio transport.
	Command string            `json:"command,omitempty"`
	Args    []string          `json:"args,omitempty"`
	Env     map[string]string `json:"env,omitempty"`
	Cwd     string            `json:"cwd,omitempty"` // optional working directory

	// HTTP transport.
	URL     string            `json:"url,omitempty"`
	Headers map[string]string `json:"headers,omitempty"`
}

// Endpoint returns a human-readable description of where the server lives: the
// URL for http servers, or the command line for stdio servers.
func (s Server) Endpoint() string {
	if s.Transport() == "http" {
		return s.URL
	}
	cmd := s.Command
	if len(s.Args) > 0 {
		cmd += " " + strings.Join(s.Args, " ")
	}
	return cmd
}

// Transport returns the transport kind for this server: "http" or "stdio".
func (s Server) Transport() string {
	switch s.Type {
	case "http", "sse", "streamable-http":
		return "http"
	case "stdio":
		return "stdio"
	}
	if s.URL != "" {
		return "http"
	}
	return "stdio"
}

// Config is the parsed configuration file.
type Config struct {
	Servers map[string]Server `json:"mcpServers"`
	// Path is the file this config was loaded from (for diagnostics).
	Path string `json:"-"`
}

// Names returns the configured server names, sorted.
func (c *Config) Names() []string {
	names := make([]string, 0, len(c.Servers))
	for name := range c.Servers {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// Get returns the named server, or an error if it is not configured. The error
// includes a "did you mean" hint when a similar name exists.
func (c *Config) Get(name string) (Server, error) {
	s, ok := c.Servers[name]
	if ok {
		return s, nil
	}
	msg := fmt.Sprintf("no MCP server named %q is configured in %s", name, c.Path)
	if suggestion := c.Suggest(name); suggestion != "" {
		msg += fmt.Sprintf("\n\nDid you mean %q? Run 'climcp mcp list' to see all servers.", suggestion)
	} else if len(c.Servers) > 0 {
		msg += "\n\nRun 'climcp mcp list' to see the configured servers."
	}
	return Server{}, fmt.Errorf("%s", msg)
}

// Suggest returns the configured server name closest to the given input (by
// edit distance), or "" if nothing is close enough to be helpful.
func (c *Config) Suggest(input string) string {
	best := ""
	bestDist := 1 << 30
	for _, name := range c.Names() {
		d := EditDistance(input, name)
		if d < bestDist {
			bestDist = d
			best = name
		}
	}
	// Only suggest when the names are genuinely close: within a third of the
	// longer name's length (and always allow a distance of 1 or 2).
	threshold := len(input)
	if len(best) > threshold {
		threshold = len(best)
	}
	threshold = threshold/3 + 1
	if bestDist <= threshold {
		return best
	}
	return ""
}

// EditDistance computes the Levenshtein edit distance between two strings.
func EditDistance(a, b string) int {
	ra, rb := []rune(a), []rune(b)
	prev := make([]int, len(rb)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(ra); i++ {
		cur := make([]int, len(rb)+1)
		cur[0] = i
		for j := 1; j <= len(rb); j++ {
			cost := 1
			if ra[i-1] == rb[j-1] {
				cost = 0
			}
			cur[j] = min3(prev[j]+1, cur[j-1]+1, prev[j-1]+cost)
		}
		prev = cur
	}
	return prev[len(rb)]
}

func min3(a, b, c int) int {
	m := a
	if b < m {
		m = b
	}
	if c < m {
		m = c
	}
	return m
}

// candidatePaths returns the default locations searched when no explicit path
// is given, in priority order.
func candidatePaths() []string {
	var paths []string
	if cwd, err := os.Getwd(); err == nil {
		paths = append(paths, filepath.Join(cwd, "climcp.json"))
	}
	if home, err := os.UserHomeDir(); err == nil {
		paths = append(paths,
			filepath.Join(home, ".config", "climcp", "config.json"),
			filepath.Join(home, ".climcp.json"),
		)
	}
	return paths
}

// Load reads the config from explicitPath if non-empty, otherwise it searches
// the default candidate locations and uses the first that exists.
func Load(explicitPath string) (*Config, error) {
	path := explicitPath
	if path == "" {
		for _, p := range candidatePaths() {
			if _, err := os.Stat(p); err == nil {
				path = p
				break
			}
		}
		if path == "" {
			return nil, fmt.Errorf(
				"no config file found; searched %v.\nCreate a climcp.json with an \"mcpServers\" object, or pass --config <path>",
				candidatePaths(),
			)
		}
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading config %s: %w", path, err)
	}

	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parsing config %s: %w", path, err)
	}
	if cfg.Servers == nil {
		cfg.Servers = map[string]Server{}
	}
	cfg.Path = path
	return &cfg, nil
}
