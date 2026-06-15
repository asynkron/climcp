# Changelog

All notable changes to this project are documented here. The format is based on
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and this project
adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added
- `climcp mcp list` — list configured servers as an aligned table showing each
  server's transport and endpoint.
- `climcp describe <server>` — connect to a server and list its operations with
  parameter signatures and descriptions.
- `climcp call "<server>.<op>(args)"` — invoke an operation. Arguments accept
  both a JSON object and a collapsed function-call form
  (`op(foo: 1, bar: 'hej')`).
- Flag aliases `--describe` and `--call`.
- Two transports: **stdio** (spawned process) and **HTTP** (Streamable HTTP,
  including `text/event-stream` responses and session reuse).
- Config compatible with the `mcpServers` / `mcp.json` shape, searched at
  `./climcp.json`, `~/.config/climcp/config.json`, and `~/.climcp.json`, or via
  `--config`.
- `--json` output for `mcp list`, `describe`, and `call`.
- `--timeout` flag (default 60s) and SIGINT/SIGTERM handling so a hung server
  can't wedge the CLI.
- "Did you mean …?" suggestions for unknown server names and commands.
- TTY-aware colored output, honoring `NO_COLOR` and `--no-color`.
- Detailed `--help` with full call grammar and worked examples.

[Unreleased]: https://github.com/asynkron/climcp/commits/main
