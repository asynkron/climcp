package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"

	"github.com/asynkron/climcp/internal/config"
)

// stdioTransport speaks newline-delimited JSON-RPC to a spawned server process.
type stdioTransport struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout *bufio.Reader
	mu     sync.Mutex
}

func newStdioTransport(ctx context.Context, srv config.Server) (transport, error) {
	if srv.Command == "" {
		return nil, fmt.Errorf("stdio server has no command configured")
	}

	cmd := exec.CommandContext(ctx, srv.Command, srv.Args...)
	cmd.Dir = srv.Cwd
	cmd.Env = os.Environ()
	for k, v := range srv.Env {
		cmd.Env = append(cmd.Env, fmt.Sprintf("%s=%s", k, v))
	}
	// Let the server's diagnostics flow to our stderr so failures are visible.
	cmd.Stderr = os.Stderr

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("opening stdin pipe: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("opening stdout pipe: %w", err)
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("starting %q: %w", srv.Command, err)
	}

	return &stdioTransport{
		cmd:    cmd,
		stdin:  stdin,
		stdout: bufio.NewReaderSize(stdout, 1<<20),
	}, nil
}

func (t *stdioTransport) roundTrip(_ context.Context, id int, message []byte) (jsonrpcResponse, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	if err := t.writeLine(message); err != nil {
		return jsonrpcResponse{}, err
	}
	// Read until we see the response with our id; skip notifications and any
	// unrelated responses that may interleave.
	for {
		var resp jsonrpcResponse
		if err := t.readMessage(&resp); err != nil {
			return jsonrpcResponse{}, err
		}
		if resp.ID == nil || *resp.ID != id {
			continue
		}
		return resp, nil
	}
}

func (t *stdioTransport) send(_ context.Context, message []byte) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.writeLine(message)
}

func (t *stdioTransport) close() error {
	if t.stdin != nil {
		_ = t.stdin.Close()
	}
	if t.cmd != nil && t.cmd.Process != nil {
		_ = t.cmd.Process.Kill()
		_ = t.cmd.Wait()
	}
	return nil
}

func (t *stdioTransport) writeLine(message []byte) error {
	// stdio transport: one JSON message per line, no embedded newlines.
	out := append(append([]byte(nil), message...), '\n')
	if _, err := t.stdin.Write(out); err != nil {
		return fmt.Errorf("writing to server: %w", err)
	}
	return nil
}

func (t *stdioTransport) readMessage(v interface{}) error {
	line, err := t.stdout.ReadBytes('\n')
	if err != nil {
		if err == io.EOF && len(line) == 0 {
			return fmt.Errorf("server closed connection unexpectedly")
		}
		if len(line) == 0 {
			return err
		}
		// Process whatever we got before the error.
	}
	return json.Unmarshal(line, v)
}
