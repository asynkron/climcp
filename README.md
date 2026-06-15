# climcp

A small CLI that acts as a bridge to **MCP servers**. Point it at a config file
(the same `mcpServers` shape agents use), and you can list the configured
servers, describe the operations each exposes, and call them from the shell.
Both **stdio** (spawned process) and **HTTP** (Streamable HTTP / SSE) servers
are supported.

## Build & install

```sh
make build      # -> ./bin/climcp
make install    # -> /usr/local/bin/climcp (override with PREFIX=...)
make test
```

Or plainly: `go build -o climcp .`

## Configure

Create a `climcp.json`. The format is compatible with the usual `mcp.json`:

```json
{
  "mcpServers": {
    "fs": {
      "command": "npx",
      "args": ["-y", "@modelcontextprotocol/server-filesystem", "/tmp"],
      "env": {}
    },
    "remote": {
      "type": "http",
      "url": "https://example.com/mcp",
      "headers": { "Authorization": "Bearer XXX" }
    }
  }
}
```

The transport is inferred: a `url` (or an explicit `type` of `http` / `sse` /
`streamable-http`) selects HTTP; otherwise the server is stdio.

- **stdio**: `command` (required), `args`, `env`, `cwd` (optional working dir).
- **http**: `url` (required), `headers` (optional). Uses the Streamable HTTP
  transport — responses delivered as `application/json` or `text/event-stream`
  are both handled, and a session id issued at `initialize` is reused.

The config is searched in this order:

1. `./climcp.json`
2. `~/.config/climcp/config.json`
3. `~/.climcp.json`

Or pass `--config <path>` to use a specific file.

## Commands

```sh
climcp --help                 # usage
climcp mcp list               # list configured servers
climcp describe fs            # show fs's operations + parameters
climcp call "fs.read_file(path: '/tmp/a.txt')"
```

Flag-style equivalents also work: `climcp --describe fs` and
`climcp --call "<expr>"`.

### Call syntax

Two equivalent argument styles inside the parentheses:

```sh
# JSON object
climcp call 'fs.read_file({"path": "/tmp/a.txt", "lines": 10})'

# collapsed function-call form — keys are bare, strings may use single quotes
climcp call "fs.read_file(path: '/tmp/a.txt', lines: 10)"
```

The collapsed form supports nested objects and arrays too:

```sh
climcp call "search.query(filter: {kind: 'file', tags: ['x', 'y']}, n: 3)"
```

Values may be quoted strings, numbers, `true`/`false`/`null`, objects, or
arrays. Bare unquoted strings are rejected — quote them.

Add `--json` to `call` to print the raw JSON result instead of the text
content.

## How it works

For `describe` / `call`, climcp spawns the configured server process, performs
the MCP `initialize` handshake over newline-delimited JSON-RPC 2.0, then issues
`tools/list` or `tools/call`. The process is shut down when the command
finishes.

## Tests

```sh
go test ./...
```
