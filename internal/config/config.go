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

// Get returns the named server, or an error if it is not configured.
func (c *Config) Get(name string) (Server, error) {
	s, ok := c.Servers[name]
	if !ok {
		return Server{}, fmt.Errorf("no MCP server named %q is configured in %s", name, c.Path)
	}
	return s, nil
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
