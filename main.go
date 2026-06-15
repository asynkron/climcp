// climcp is a small CLI that bridges to configured stdio MCP servers: it can
// list them, describe their operations, and call those operations.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"sort"
	"strings"
	"syscall"

	"github.com/asynkron/climcp/internal/callexpr"
	"github.com/asynkron/climcp/internal/config"
	"github.com/asynkron/climcp/internal/mcp"
)

// version is overridden at build time via -ldflags "-X main.version=...".
var version = "dev"

const usage = `climcp - a bridge to MCP servers (stdio and http)

  Lists configured Model Context Protocol servers, describes the operations
  each one exposes, and calls those operations from your shell.

USAGE
  climcp <command> [arguments] [flags]
  climcp [flags]

COMMANDS
  mcp list                     List the servers defined in your config.
  describe <server>            Connect to <server> and list its operations,
                               each with its parameters and description.
  call "<server>.<op>(args)"   Connect to <server> and invoke operation <op>
                               with the given arguments. Quote the whole
                               expression so the shell keeps it as one word.
  help, --help, -h             Show this help.
  version, --version, -v       Print the climcp version.

  Equivalent flag forms (handy for scripts):
    climcp --describe <server>          same as: climcp describe <server>
    climcp --call "<server>.<op>(...)"  same as: climcp call "<server>.<op>(...)"

GLOBAL FLAGS
  --config <path>   Use a specific config file instead of searching the
                    default locations. May also be written --config=<path>.
  --json            For 'call', print the raw JSON-RPC result (pretty-printed)
                    instead of just the text content. Useful for piping to jq.

CALL SYNTAX
  An expression has three parts:

    <server> . <operation> ( <arguments> )
       |          |              |
       |          |              +-- arguments, in one of the two styles below
       |          +----------------- the operation/tool name (from 'describe')
       +---------------------------- a server name (from 'mcp list')

  The argument list inside the parentheses accepts TWO equivalent styles:

  1) JSON object  - a single, standard JSON object:
       fs.read_file({"path": "/tmp/a.txt", "tail": 20})

  2) Collapsed    - function-call style; the outer braces are dropped and
                    keys are written bare, like named arguments:
       fs.read_file(path: '/tmp/a.txt', tail: 20)

  Both produce the same call. The collapsed form is lenient:
    - keys      : bare identifiers (foo, my-key, a.b) or quoted ("foo")
    - strings   : single OR double quoted          'hej'   "hej"
    - numbers   : 1, -3, 1.5                        (unquoted)
    - booleans  : true, false                       (unquoted)
    - null      : null                              (unquoted)
    - objects   : {kind: 'file', size: 10}          (may nest)
    - arrays    : ['a', 'b', 3]                      (may nest)
    - a trailing comma before ) or } is allowed
  Bare/unquoted strings are rejected on purpose - always quote string values.

  No arguments? Use empty parentheses:
       time.now()

EXAMPLES
  # List configured servers
  climcp mcp list

  # Use a config that isn't in a default location
  climcp --config ./my-servers.json mcp list

  # Discover what a server can do
  climcp describe fs

  # Call with the collapsed (named-argument) style
  climcp call "fs.read_file(path: '/etc/hosts')"

  # The same call, JSON style (note the single quotes around the whole arg
  # so the shell doesn't touch the double quotes inside)
  climcp call 'fs.read_file({"path": "/etc/hosts"})'

  # Nested object and array arguments
  climcp call "search.query(filter: {kind: 'file', tags: ['go', 'cli']}, limit: 5)"

  # No arguments
  climcp call "time.now()"

  # Get the raw JSON result and pipe it to jq
  climcp --json call "fs.list_directory(path: '/tmp')" | jq '.content'

  # Call a remote HTTP server defined in the config
  climcp call "remote.search(q: 'mcp')"

CONFIG FILE
  Format is compatible with the usual mcp.json "mcpServers" shape. A server is
  either stdio (a spawned child process) or http (a remote URL). The transport
  is inferred: a "url" - or "type" of http/sse/streamable-http - means http;
  otherwise stdio.

  {
    "mcpServers": {
      "fs": {                                  // stdio server
        "command": "npx",                      //   required: executable
        "args": ["-y",                         //   optional: arguments
                 "@modelcontextprotocol/server-filesystem", "/tmp"],
        "env": { "LOG_LEVEL": "info" },         //   optional: extra env vars
        "cwd": "/path/to/workdir"               //   optional: working directory
      },
      "remote": {                              // http server
        "type": "http",                         //   optional but explicit
        "url": "https://example.com/mcp",       //   required: endpoint URL
        "headers": {                            //   optional: sent on every call
          "Authorization": "Bearer XXX"
        }
      }
    }
  }

CONFIG SEARCH ORDER
  When --config is not given, the first file that exists is used:
    1. ./climcp.json                   (current directory)
    2. ~/.config/climcp/config.json
    3. ~/.climcp.json

EXIT STATUS
  0   success
  1   error (bad arguments, config/connection failure, or the operation
      itself reported an error)
`

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "climcp: "+err.Error())
		os.Exit(1)
	}
}

// parsedArgs holds the flags pulled out of the raw argv, plus the leftovers.
type parsedArgs struct {
	configPath string
	jsonOut    bool
	describe   string // set by --describe
	call       string // set by --call
	rest       []string
}

func parseFlags(argv []string) (*parsedArgs, error) {
	p := &parsedArgs{}
	for i := 0; i < len(argv); i++ {
		arg := argv[i]
		switch {
		case arg == "--help" || arg == "-h" || arg == "help":
			fmt.Print(usage)
			os.Exit(0)
		case arg == "--version" || arg == "-v" || arg == "version":
			fmt.Printf("climcp %s\n", version)
			os.Exit(0)
		case arg == "--json":
			p.jsonOut = true
		case arg == "--config":
			if i+1 >= len(argv) {
				return nil, fmt.Errorf("--config requires a path argument")
			}
			i++
			p.configPath = argv[i]
		case strings.HasPrefix(arg, "--config="):
			p.configPath = strings.TrimPrefix(arg, "--config=")
		case arg == "--describe":
			if i+1 >= len(argv) {
				return nil, fmt.Errorf("--describe requires a server name")
			}
			i++
			p.describe = argv[i]
		case strings.HasPrefix(arg, "--describe="):
			p.describe = strings.TrimPrefix(arg, "--describe=")
		case arg == "--call":
			if i+1 >= len(argv) {
				return nil, fmt.Errorf("--call requires an expression")
			}
			i++
			p.call = argv[i]
		case strings.HasPrefix(arg, "--call="):
			p.call = strings.TrimPrefix(arg, "--call=")
		default:
			p.rest = append(p.rest, arg)
		}
	}
	return p, nil
}

func run(argv []string) error {
	if len(argv) == 0 {
		fmt.Print(usage)
		return nil
	}

	p, err := parseFlags(argv)
	if err != nil {
		return err
	}

	// Flag-style invocations take precedence when present.
	if p.describe != "" {
		return cmdDescribe(p.configPath, p.describe)
	}
	if p.call != "" {
		return cmdCall(p.configPath, p.call, p.jsonOut)
	}

	if len(p.rest) == 0 {
		fmt.Print(usage)
		return nil
	}

	switch p.rest[0] {
	case "mcp":
		if len(p.rest) >= 2 && p.rest[1] == "list" {
			return cmdList(p.configPath)
		}
		return fmt.Errorf("unknown 'mcp' subcommand; did you mean 'mcp list'?")
	case "list":
		return cmdList(p.configPath)
	case "describe":
		if len(p.rest) < 2 {
			return fmt.Errorf("describe requires a server name: climcp describe <server>")
		}
		return cmdDescribe(p.configPath, p.rest[1])
	case "call":
		if len(p.rest) < 2 {
			return fmt.Errorf("call requires an expression: climcp call \"<server>.<op>(args)\"")
		}
		return cmdCall(p.configPath, p.rest[1], p.jsonOut)
	default:
		return fmt.Errorf("unknown command %q (try: climcp --help)", p.rest[0])
	}
}

func cmdList(configPath string) error {
	cfg, err := config.Load(configPath)
	if err != nil {
		return err
	}
	names := cfg.Names()
	if len(names) == 0 {
		fmt.Printf("No MCP servers configured in %s\n", cfg.Path)
		return nil
	}
	fmt.Printf("Configured MCP servers (%s):\n\n", cfg.Path)
	for _, name := range names {
		s := cfg.Servers[name]
		cmdline := s.Command
		if len(s.Args) > 0 {
			cmdline += " " + strings.Join(s.Args, " ")
		}
		fmt.Printf("  %-20s %s\n", name, cmdline)
	}
	return nil
}

func cmdDescribe(configPath, name string) error {
	cfg, err := config.Load(configPath)
	if err != nil {
		return err
	}
	srv, err := cfg.Get(name)
	if err != nil {
		return err
	}

	ctx, cancel := signalContext()
	defer cancel()

	client, err := mcp.Connect(ctx, srv)
	if err != nil {
		return err
	}
	defer client.Close()

	tools, err := client.ListTools()
	if err != nil {
		return err
	}

	info := client.ServerInfo()
	if info.Name != "" {
		fmt.Printf("%s (%s %s)\n\n", name, info.Name, info.Version)
	} else {
		fmt.Printf("%s\n\n", name)
	}
	if len(tools) == 0 {
		fmt.Println("  (no operations exposed)")
		return nil
	}
	for _, t := range tools {
		fmt.Printf("  %s(%s)\n", t.Name, summarizeParams(t.InputSchema))
		if t.Description != "" {
			for _, line := range strings.Split(strings.TrimSpace(t.Description), "\n") {
				fmt.Printf("      %s\n", line)
			}
		}
		fmt.Println()
	}
	return nil
}

// summarizeParams renders an input JSON Schema as a compact parameter list,
// e.g. "path: string, recursive?: boolean".
func summarizeParams(schema json.RawMessage) string {
	if len(schema) == 0 {
		return ""
	}
	var s struct {
		Properties map[string]struct {
			Type        interface{} `json:"type"`
			Description string      `json:"description"`
		} `json:"properties"`
		Required []string `json:"required"`
	}
	if err := json.Unmarshal(schema, &s); err != nil || len(s.Properties) == 0 {
		return ""
	}
	required := map[string]bool{}
	for _, r := range s.Required {
		required[r] = true
	}
	names := make([]string, 0, len(s.Properties))
	for n := range s.Properties {
		names = append(names, n)
	}
	sort.Strings(names)

	parts := make([]string, 0, len(names))
	for _, n := range names {
		opt := ""
		if !required[n] {
			opt = "?"
		}
		parts = append(parts, fmt.Sprintf("%s%s: %s", n, opt, typeName(s.Properties[n].Type)))
	}
	return strings.Join(parts, ", ")
}

func typeName(t interface{}) string {
	switch v := t.(type) {
	case string:
		return v
	case []interface{}:
		strs := make([]string, 0, len(v))
		for _, item := range v {
			if s, ok := item.(string); ok {
				strs = append(strs, s)
			}
		}
		if len(strs) > 0 {
			return strings.Join(strs, "|")
		}
	}
	return "any"
}

func cmdCall(configPath, expr string, jsonOut bool) error {
	call, err := callexpr.Parse(expr)
	if err != nil {
		return err
	}

	cfg, err := config.Load(configPath)
	if err != nil {
		return err
	}
	srv, err := cfg.Get(call.Server)
	if err != nil {
		return err
	}

	ctx, cancel := signalContext()
	defer cancel()

	client, err := mcp.Connect(ctx, srv)
	if err != nil {
		return err
	}
	defer client.Close()

	result, err := client.CallTool(call.Operation, call.Arguments)
	if err != nil {
		return err
	}

	if jsonOut {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(result); err != nil {
			return err
		}
	} else {
		printResult(result)
	}

	if result.IsError {
		return fmt.Errorf("the operation reported an error (see output above)")
	}
	return nil
}

func printResult(result *mcp.CallToolResult) {
	for _, block := range result.Content {
		switch block.Type {
		case "text":
			fmt.Println(block.Text)
		default:
			// Non-text content: dump the raw block so nothing is lost.
			fmt.Println(string(block.Raw))
		}
	}
	if len(result.Content) == 0 && len(result.StructuredContent) > 0 {
		fmt.Println(string(result.StructuredContent))
	}
}

// signalContext returns a context cancelled on SIGINT/SIGTERM so a hung server
// doesn't leave climcp wedged.
func signalContext() (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithCancel(context.Background())
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-ch
		cancel()
	}()
	return ctx, cancel
}
