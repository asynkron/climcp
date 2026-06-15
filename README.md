# climcp

[![CI](https://github.com/asynkron/climcp/actions/workflows/ci.yml/badge.svg)](https://github.com/asynkron/climcp/actions/workflows/ci.yml)
[![Go Reference](https://pkg.go.dev/badge/github.com/asynkron/climcp.svg)](https://pkg.go.dev/github.com/asynkron/climcp)
[![Go Report Card](https://goreportcard.com/badge/github.com/asynkron/climcp)](https://goreportcard.com/report/github.com/asynkron/climcp)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)

A small CLI that bridges your shell to **MCP servers** (Model Context Protocol).
Point it at a config file — the same `mcpServers` shape agents already use — and
you can list the configured servers, describe the operations each exposes, and
call those operations directly from the command line.

Both **stdio** (a spawned child process) and **HTTP** (Streamable HTTP / SSE)
servers are supported.

```console
$ climcp mcp list
2 MCP servers configured in ./climcp.json

  NAME  TRANSPORT  ENDPOINT
  fs    stdio      npx -y @modelcontextprotocol/server-filesystem /tmp
  docs  http       https://example.com/mcp

$ climcp call "fs.read_file(path: '/etc/hostname')"
my-machine
```

## Install

```sh
# from source, into ./bin
make build

# install to /usr/local/bin (override with PREFIX=...)
make install

# or with the Go toolchain
go install github.com/asynkron/climcp@latest
```

Pre-built binaries for Linux, macOS, and Windows are attached to each
[release](https://github.com/asynkron/climcp/releases).

## Configure

Create a `climcp.json`. The format is compatible with the usual `mcp.json`:

```json
{
  "mcpServers": {
    "fs": {
      "command": "npx",
      "args": ["-y", "@modelcontextprotocol/server-filesystem", "/tmp"],
      "env": { "LOG_LEVEL": "info" },
      "cwd": "/optional/working/dir"
    },
    "docs": {
      "type": "http",
      "url": "https://example.com/mcp",
      "headers": { "Authorization": "Bearer XXX" }
    }
  }
}
```

The transport is **inferred**: a `url` — or an explicit `type` of `http` /
`sse` / `streamable-http` — selects HTTP; otherwise the server is stdio.

| Transport | Required | Optional |
|-----------|----------|----------|
| **stdio** | `command` | `args`, `env`, `cwd` |
| **http**  | `url`     | `headers` |

The HTTP transport uses Streamable HTTP: responses delivered as
`application/json` or `text/event-stream` are both handled, and a session id
issued at `initialize` is reused on later requests.

When `--config` is not given, the first existing file is used, in order:

1. `./climcp.json`
2. `~/.config/climcp/config.json`
3. `~/.climcp.json`

### Reuse or import an existing config

Because the format matches the usual `mcpServers` shape, you can point climcp
straight at an agent's config without changing anything:

```sh
climcp --config ~/.cursor/mcp.json mcp list
```

Or import its servers into your own `climcp.json` so you don't have to repeat
`--config`:

```sh
climcp import ~/.cursor/mcp.json            # merge into ./climcp.json
climcp import ~/.cursor/mcp.json --to ~/.config/climcp/config.json
climcp import ~/.cursor/mcp.json --overwrite --dry-run   # preview replacements
```

Import accepts both the `mcpServers` and `servers` config shapes. Name clashes
are skipped by default (reported), unless you pass `--overwrite`.

## Commands

| Command | Description |
|---------|-------------|
| `climcp mcp list` | List configured servers (name, transport, endpoint). |
| `climcp describe <server>` | Connect and list the server's operations and parameters. |
| `climcp call "<server>.<op>(args)"` | Invoke an operation with arguments. |
| `climcp import <file>` | Merge servers from an existing config into your `climcp.json`. |
| `climcp --help` | Detailed help — also lists your configured servers and the next steps. |
| `climcp --version` | Print the version. |

Flag-style aliases also work: `climcp --describe <server>` and
`climcp --call "<expr>"`.

### Global flags

| Flag | Description |
|------|-------------|
| `--config <path>` | Use a specific config file. |
| `--json` | Emit JSON instead of formatted text (works with all three commands). |
| `--timeout <dur>` | Abort if the server is unresponsive (default `60s`; e.g. `30s`, `2m`). |
| `--no-color` | Disable colored output (also honors the `NO_COLOR` env var). |

Colors are used automatically only when writing to a terminal; piped or
redirected output is always plain.

## Call syntax

A call expression has three parts: `<server>.<operation>(<arguments>)`. The
arguments accept **two equivalent styles**:

```sh
# 1) JSON object
climcp call 'fs.read_file({"path": "/tmp/a.txt", "tail": 20})'

# 2) collapsed function-call form — bare keys, single or double quotes
climcp call "fs.read_file(path: '/tmp/a.txt', tail: 20)"
```

The collapsed form supports nested objects and arrays:

```sh
climcp call "search.query(filter: {kind: 'file', tags: ['go', 'cli']}, limit: 5)"
```

Values may be quoted strings, numbers, `true` / `false` / `null`, objects, or
arrays. Bare unquoted strings are rejected on purpose — always quote string
values. Use empty parentheses for no arguments: `climcp call "time.now()"`.

Pipe the raw result into `jq`:

```sh
climcp --json call "fs.list_directory(path: '/tmp')" | jq '.content'
```

## How it works

For `describe` and `call`, climcp opens the configured transport (spawning the
child process for stdio, or POSTing to the URL for HTTP), performs the MCP
`initialize` handshake over JSON-RPC 2.0, then issues `tools/list` or
`tools/call`. A stdio child is shut down when the command finishes, and the
whole operation is bounded by `--timeout` and cancelled on Ctrl-C.

## Development

```sh
make test     # go test ./...
make vet      # go vet ./...
make fmt      # gofmt -w .
make build    # -> ./bin/climcp
```

`testdata/mockserver` is a tiny stdio MCP server used by the end-to-end tests.

## License

[MIT](LICENSE) © Asynkron
