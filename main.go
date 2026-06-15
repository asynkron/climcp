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
	"text/tabwriter"
	"time"

	"github.com/asynkron/climcp/internal/callexpr"
	"github.com/asynkron/climcp/internal/config"
	"github.com/asynkron/climcp/internal/mcp"
	"github.com/asynkron/climcp/internal/ui"
)

// version is overridden at build time via -ldflags "-X main.version=...".
var version = "dev"

const usage = `climcp - a bridge to MCP servers (stdio and http)

  Lists configured Model Context Protocol servers, describes the operations
  each one exposes, and calls those operations from your shell.

USAGE
  climcp <command> [arguments] [flags]
  climcp [flags]

GETTING STARTED
  climcp reads a JSON config listing your MCP servers (see CONFIG FILE below).
  Three ways to get one:

    1. Write a ./climcp.json yourself with an "mcpServers" object.
    2. Reuse an existing agent config as-is - the format matches, so you can
       point straight at it:
         climcp --config ~/.cursor/mcp.json mcp list
    3. Import servers from an existing config into your own climcp.json:
         climcp import ~/.cursor/mcp.json

COMMANDS
  mcp list                     List the servers defined in your config.
  describe <server>            Connect to <server> and list its operations,
                               each with its parameters and description.
  call "<server>.<op>(args)"   Connect to <server> and invoke operation <op>
                               with the given arguments. Quote the whole
                               expression so the shell keeps it as one word.
  import <file>                Merge the servers from <file> into your climcp
                               config (default ./climcp.json). Accepts the
                               "mcpServers" and "servers" config shapes.
  help, --help, -h             Show this help.
  version, --version, -v       Print the climcp version.

  Equivalent flag forms (handy for scripts):
    climcp --describe <server>          same as: climcp describe <server>
    climcp --call "<server>.<op>(...)"  same as: climcp call "<server>.<op>(...)"
    climcp --import <file>              same as: climcp import <file>

IMPORT FLAGS
  --to <path>       Destination config to merge into (default ./climcp.json).
  --overwrite       Replace servers whose names already exist (default: skip).
  --dry-run         Show what would change without writing anything.

GLOBAL FLAGS
  --config <path>   Use a specific config file instead of searching the
                    default locations. May also be written --config=<path>.
  --json            Print machine-readable JSON instead of formatted text.
                    Works with 'mcp list', 'describe', and 'call'.
  --timeout <dur>   Abort if a server doesn't respond within this duration
                    (default 60s). Accepts Go durations: 500ms, 30s, 2m.
  --no-color        Disable colored output (or set the NO_COLOR env var).

  Colors are used automatically only when writing to a terminal; piped or
  redirected output is always plain.

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

  # Import servers from an existing agent config into ./climcp.json
  climcp import ~/.cursor/mcp.json

  # Preview an import without writing, replacing any name clashes
  climcp import ~/.cursor/mcp.json --overwrite --dry-run

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
		prefix := "climcp:"
		if info, statErr := os.Stderr.Stat(); statErr == nil && info.Mode()&os.ModeCharDevice != 0 && ui.Enabled() {
			prefix = ui.Red(prefix)
		}
		fmt.Fprintln(os.Stderr, prefix+" "+err.Error())
		os.Exit(1)
	}
}

// options holds the global flags shared by every command.
type options struct {
	configPath string
	jsonOut    bool
	timeout    time.Duration
}

// parsedArgs holds the flags pulled out of the raw argv, plus the leftovers.
type parsedArgs struct {
	opts       options
	describe   string // set by --describe
	call       string // set by --call
	importFile string // set by --import
	importTo   string // set by --to
	overwrite  bool   // set by --overwrite
	dryRun     bool   // set by --dry-run
	rest       []string
}

func parseFlags(argv []string) (*parsedArgs, error) {
	p := &parsedArgs{opts: options{timeout: 60 * time.Second}}

	// needValue returns the value for a flag, supporting both "--flag value"
	// and "--flag=value" forms.
	for i := 0; i < len(argv); i++ {
		arg := argv[i]
		valueFor := func(name string) (string, bool, error) {
			if arg == name {
				if i+1 >= len(argv) {
					return "", false, fmt.Errorf("%s requires a value", name)
				}
				i++
				return argv[i], true, nil
			}
			if strings.HasPrefix(arg, name+"=") {
				return strings.TrimPrefix(arg, name+"="), true, nil
			}
			return "", false, nil
		}

		switch {
		case arg == "--help" || arg == "-h" || arg == "help":
			fmt.Print(usage)
			os.Exit(0)
		case arg == "--version" || arg == "-v" || arg == "version":
			fmt.Printf("climcp %s\n", version)
			os.Exit(0)
		case arg == "--json":
			p.opts.jsonOut = true
		case arg == "--no-color":
			ui.SetEnabled(false)
		case arg == "--overwrite":
			p.overwrite = true
		case arg == "--dry-run":
			p.dryRun = true
		default:
			if v, ok, err := valueFor("--config"); err != nil {
				return nil, err
			} else if ok {
				p.opts.configPath = v
				continue
			}
			if v, ok, err := valueFor("--timeout"); err != nil {
				return nil, err
			} else if ok {
				d, perr := time.ParseDuration(v)
				if perr != nil {
					return nil, fmt.Errorf("invalid --timeout %q: %v (use e.g. 30s, 2m)", v, perr)
				}
				p.opts.timeout = d
				continue
			}
			if v, ok, err := valueFor("--describe"); err != nil {
				return nil, err
			} else if ok {
				p.describe = v
				continue
			}
			if v, ok, err := valueFor("--call"); err != nil {
				return nil, err
			} else if ok {
				p.call = v
				continue
			}
			if v, ok, err := valueFor("--import"); err != nil {
				return nil, err
			} else if ok {
				p.importFile = v
				continue
			}
			if v, ok, err := valueFor("--to"); err != nil {
				return nil, err
			} else if ok {
				p.importTo = v
				continue
			}
			if strings.HasPrefix(arg, "-") {
				return nil, fmt.Errorf("unknown flag %q (try: climcp --help)", arg)
			}
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
		return cmdDescribe(p.opts, p.describe)
	}
	if p.call != "" {
		return cmdCall(p.opts, p.call)
	}
	if p.importFile != "" {
		return cmdImport(p, p.importFile)
	}

	if len(p.rest) == 0 {
		fmt.Print(usage)
		return nil
	}

	switch p.rest[0] {
	case "mcp":
		if len(p.rest) >= 2 && p.rest[1] == "list" {
			return cmdList(p.opts)
		}
		if len(p.rest) >= 2 {
			return fmt.Errorf("unknown 'mcp' subcommand %q; did you mean 'mcp list'?", p.rest[1])
		}
		return fmt.Errorf("'mcp' needs a subcommand; did you mean 'mcp list'?")
	case "list":
		return cmdList(p.opts)
	case "describe":
		if len(p.rest) < 2 {
			return fmt.Errorf("describe requires a server name: climcp describe <server>")
		}
		return cmdDescribe(p.opts, p.rest[1])
	case "call":
		if len(p.rest) < 2 {
			return fmt.Errorf("call requires an expression: climcp call \"<server>.<op>(args)\"")
		}
		return cmdCall(p.opts, p.rest[1])
	case "import":
		if len(p.rest) < 2 {
			return fmt.Errorf("import requires a source file: climcp import <file>")
		}
		return cmdImport(p, p.rest[1])
	default:
		msg := fmt.Sprintf("unknown command %q", p.rest[0])
		if s := suggestCommand(p.rest[0]); s != "" {
			msg += fmt.Sprintf("; did you mean %q?", s)
		}
		return fmt.Errorf("%s (try: climcp --help)", msg)
	}
}

// suggestCommand returns the known command closest to input, if any is close.
func suggestCommand(input string) string {
	const maxDist = 2
	best, bestDist := "", maxDist+1
	for _, c := range []string{"mcp", "list", "describe", "call", "import", "help", "version"} {
		if d := config.EditDistance(input, c); d < bestDist {
			best, bestDist = c, d
		}
	}
	if bestDist <= maxDist {
		return best
	}
	return ""
}

func cmdList(opts options) error {
	cfg, err := config.Load(opts.configPath)
	if err != nil {
		return err
	}
	names := cfg.Names()

	if opts.jsonOut {
		return printJSON(cfg.Servers)
	}

	if len(names) == 0 {
		fmt.Printf("No MCP servers configured in %s\n", ui.Dim(cfg.Path))
		fmt.Println("\nAdd an \"mcpServers\" object to that file. See: climcp --help")
		return nil
	}

	plural := "servers"
	if len(names) == 1 {
		plural = "server"
	}
	fmt.Printf("%d MCP %s configured in %s\n\n", len(names), plural, ui.Dim(cfg.Path))

	w := tabwriter.NewWriter(os.Stdout, 0, 2, 2, ' ', 0)
	fmt.Fprintf(w, "  %s\t%s\t%s\n", ui.Dim("NAME"), ui.Dim("TRANSPORT"), ui.Dim("ENDPOINT"))
	for _, name := range names {
		s := cfg.Servers[name]
		fmt.Fprintf(w, "  %s\t%s\t%s\n", ui.Cyan(name), s.Transport(), s.Endpoint())
	}
	w.Flush()
	return nil
}

func cmdDescribe(opts options, name string) error {
	cfg, err := config.Load(opts.configPath)
	if err != nil {
		return err
	}
	srv, err := cfg.Get(name)
	if err != nil {
		return err
	}

	ctx, cancel := commandContext(opts.timeout)
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

	if opts.jsonOut {
		return printJSON(tools)
	}

	info := client.ServerInfo()
	header := ui.Bold(ui.Cyan(name))
	if info.Name != "" {
		header += ui.Dim(fmt.Sprintf("  (%s %s)", info.Name, info.Version))
	}
	fmt.Println(header)
	count := "no operations"
	if len(tools) == 1 {
		count = "1 operation"
	} else if len(tools) > 1 {
		count = fmt.Sprintf("%d operations", len(tools))
	}
	fmt.Printf("%s, via %s\n\n", ui.Dim(count), ui.Dim(srv.Transport()))

	if len(tools) == 0 {
		return nil
	}
	for _, t := range tools {
		fmt.Printf("  %s%s\n", ui.Green(t.Name), formatParams(t.InputSchema))
		if d := strings.TrimSpace(t.Description); d != "" {
			for _, line := range strings.Split(d, "\n") {
				fmt.Printf("      %s\n", ui.Dim(line))
			}
		}
		fmt.Println()
	}
	fmt.Printf("%s climcp call \"%s.<operation>(...)\"\n", ui.Dim("Call with:"), name)
	return nil
}

// formatParams renders the parameter list with the server name suffix coloring
// matched to the rest of the signature.
func formatParams(schema json.RawMessage) string {
	return "(" + summarizeParams(schema) + ")"
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

func cmdCall(opts options, expr string) error {
	call, err := callexpr.Parse(expr)
	if err != nil {
		return err
	}

	cfg, err := config.Load(opts.configPath)
	if err != nil {
		return err
	}
	srv, err := cfg.Get(call.Server)
	if err != nil {
		return err
	}

	ctx, cancel := commandContext(opts.timeout)
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

	if opts.jsonOut {
		if err := printJSON(result); err != nil {
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

// cmdImport merges the servers defined in a source config file into a climcp
// config file (default ./climcp.json).
func cmdImport(p *parsedArgs, source string) error {
	incoming, err := config.ParseServers(source)
	if err != nil {
		return err
	}
	if len(incoming) == 0 {
		return fmt.Errorf("no MCP servers found in %s (expected an \"mcpServers\" or \"servers\" object)", source)
	}

	dest := p.importTo
	if dest == "" {
		dest = "climcp.json"
	}

	// Start from the destination's current contents, if it exists.
	existing := map[string]config.Server{}
	if data, err := config.ParseServers(dest); err == nil {
		existing = data
	}

	var added, overwritten, skipped []string
	for _, name := range sortedKeys(incoming) {
		_, clash := existing[name]
		switch {
		case !clash:
			existing[name] = incoming[name]
			added = append(added, name)
		case p.overwrite:
			existing[name] = incoming[name]
			overwritten = append(overwritten, name)
		default:
			skipped = append(skipped, name)
		}
	}

	action := "Imported"
	if p.dryRun {
		action = "Would import"
	}
	fmt.Printf("%s from %s into %s\n", action, ui.Dim(source), ui.Dim(dest))
	reportNames(ui.Green("  + added:      "), added)
	reportNames(ui.Yellow("  ~ overwritten:"), overwritten)
	if len(skipped) > 0 {
		reportNames(ui.Dim("  - skipped:    "), skipped)
		fmt.Println(ui.Dim("    (already present; pass --overwrite to replace them)"))
	}

	if p.dryRun {
		fmt.Println(ui.Dim("\nDry run: no files were written."))
		return nil
	}
	if len(added) == 0 && len(overwritten) == 0 {
		fmt.Println(ui.Dim("\nNothing to write."))
		return nil
	}
	if err := config.Save(dest, existing); err != nil {
		return err
	}
	fmt.Printf("\nWrote %s\n", dest)
	return nil
}

// reportNames prints "label a, b, c" only when there are names to report.
func reportNames(label string, names []string) {
	if len(names) == 0 {
		return
	}
	fmt.Printf("%s %s\n", label, strings.Join(names, ", "))
}

// sortedKeys returns the map keys in sorted order.
func sortedKeys(m map[string]config.Server) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// printJSON pretty-prints v as JSON to stdout.
func printJSON(v interface{}) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
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

// commandContext returns a context cancelled on SIGINT/SIGTERM and after the
// given timeout, so a hung or unresponsive server can't leave climcp wedged.
// A non-positive timeout means no deadline.
func commandContext(timeout time.Duration) (context.Context, context.CancelFunc) {
	base := context.Background()
	var cancelTimeout context.CancelFunc = func() {}
	if timeout > 0 {
		base, cancelTimeout = context.WithTimeout(base, timeout)
	}
	ctx, cancel := context.WithCancel(base)

	ch := make(chan os.Signal, 1)
	signal.Notify(ch, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-ch
		cancel()
	}()
	return ctx, func() {
		signal.Stop(ch)
		cancel()
		cancelTimeout()
	}
}
