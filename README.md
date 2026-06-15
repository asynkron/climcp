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

## Why — stop letting MCP servers eat your context

When you wire MCP servers directly into an agent, every server dumps the full
definition of **every** tool it exposes — names, descriptions, and JSON
schemas — into the model's context window, on every single turn. Connect a
handful of servers and you've burned thousands of tokens before the agent has
done anything. That context is gone: it's not available for the actual task,
and you pay for it on every request.

`climcp` moves all of that out of the model and into the shell. The agent
doesn't preload anything. It:

- discovers servers on demand (`climcp mcp list`),
- looks up a server's operations only when it needs them (`climcp describe X`),
- and calls an operation as a plain shell command (`climcp call "X.op(...)"`).

The only thing that ever enters the context window is the specific call the
agent chose to make and the result it got back. **No always-on tool schemas, no
per-turn overhead — all that context is freed up for the work that matters.**

And because it's just a CLI writing to stdout, you can **compose** calls with
ordinary shell tooling — pipe results through `jq`, feed one call's output into
the next, loop over them — which the MCP protocol itself cannot do. See
[Chaining calls](#chaining-calls).

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

## Chaining calls

This is the part the MCP protocol can't do for you. Because every call is just
a command that writes JSON to stdout, you can **fetch → transform → fetch →
transform** in a single shell pipeline, with the intermediate data living in the
shell instead of being shuttled back through the model's context.

A tool's payload is usually JSON encoded inside a text content block, so the
recurring move is `jq -r '.content[0].text'` to unwrap it, then a second `jq` to
reshape it.

```sh
# 1. Fetch open issues from a GitHub MCP server
# 2. jq out the number of the most recently updated one
# 3. Fetch that issue's full details from the MCP
# 4. jq the final shape we care about
issue=$(climcp --json call "github.list_issues(repo: 'asynkron/climcp', state: 'open')" \
  | jq -r '.content[0].text' \
  | jq 'sort_by(.updated_at) | last | .number')

climcp --json call "github.get_issue(repo: 'asynkron/climcp', number: $issue)" \
  | jq -r '.content[0].text' \
  | jq '{title, author: .user.login, comments}'
```

Each step is independent and composable: swap a `jq` filter, redirect to a file,
`xargs` the results into N parallel calls, feed one server's output into a
different server. None of this is expressible in the MCP protocol, where the
agent would have to carry every intermediate result through its own context just
to hand it to the next tool call.

```sh
# Cross-server: read a list of URLs from disk via one MCP, fetch each via another
climcp --json call "fs.read_text_file(path: '/tmp/urls.txt')" \
  | jq -r '.content[0].text' \
  | while read -r url; do
      climcp --json call "fetch.fetch(url: '$url')" | jq -r '.content[0].text | length'
    done
```

> The operation and field names above (`github.list_issues`, `fetch.fetch`, …)
> are illustrative — run `climcp describe <server>` to see the real ones.

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
