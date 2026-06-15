package mcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/asynkron/climcp/internal/config"
)

// httpTransport implements the MCP Streamable HTTP transport. Each JSON-RPC
// message is POSTed to the configured URL; the server may answer with a single
// JSON object (application/json) or a stream of events (text/event-stream).
// A session id, if issued on initialize, is echoed on subsequent requests.
type httpTransport struct {
	url     string
	headers map[string]string
	client  *http.Client

	sessionID string
}

func newHTTPTransport(srv config.Server) (transport, error) {
	if srv.URL == "" {
		return nil, fmt.Errorf("http server has no url configured")
	}
	return &httpTransport{
		url:     srv.URL,
		headers: srv.Headers,
		client:  &http.Client{},
	}, nil
}

func (t *httpTransport) roundTrip(ctx context.Context, id int, message []byte) (jsonrpcResponse, error) {
	resp, err := t.post(ctx, message)
	if err != nil {
		return jsonrpcResponse{}, err
	}
	defer resp.Body.Close()

	// The server may hand back a session id on initialize; keep it.
	if sid := resp.Header.Get("Mcp-Session-Id"); sid != "" {
		t.sessionID = sid
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<10))
		return jsonrpcResponse{}, fmt.Errorf("http %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}

	ct := resp.Header.Get("Content-Type")
	switch {
	case strings.HasPrefix(ct, "text/event-stream"):
		return t.readSSE(resp.Body, id)
	default:
		return t.readJSON(resp.Body, id)
	}
}

func (t *httpTransport) send(ctx context.Context, message []byte) error {
	resp, err := t.post(ctx, message)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<20))
	if sid := resp.Header.Get("Mcp-Session-Id"); sid != "" {
		t.sessionID = sid
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("http %s sending notification", resp.Status)
	}
	return nil
}

func (t *httpTransport) close() error { return nil }

func (t *httpTransport) post(ctx context.Context, message []byte) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, t.url, bytes.NewReader(message))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	req.Header.Set("MCP-Protocol-Version", protocolVersion)
	if t.sessionID != "" {
		req.Header.Set("Mcp-Session-Id", t.sessionID)
	}
	for k, v := range t.headers {
		req.Header.Set(k, v)
	}
	return t.client.Do(req)
}

// readJSON parses a single JSON-RPC response (or a batch) and returns the one
// matching id.
func (t *httpTransport) readJSON(body io.Reader, id int) (jsonrpcResponse, error) {
	data, err := io.ReadAll(body)
	if err != nil {
		return jsonrpcResponse{}, err
	}
	data = bytes.TrimSpace(data)
	if len(data) == 0 {
		return jsonrpcResponse{}, fmt.Errorf("empty response from server")
	}
	if data[0] == '[' {
		var batch []jsonrpcResponse
		if err := json.Unmarshal(data, &batch); err != nil {
			return jsonrpcResponse{}, err
		}
		for _, r := range batch {
			if r.ID != nil && *r.ID == id {
				return r, nil
			}
		}
		return jsonrpcResponse{}, fmt.Errorf("no response with id %d in batch", id)
	}
	var resp jsonrpcResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return jsonrpcResponse{}, err
	}
	return resp, nil
}

// readSSE consumes an event stream, returning the first JSON-RPC message whose
// id matches. Server-initiated notifications/requests are ignored.
func (t *httpTransport) readSSE(body io.Reader, id int) (jsonrpcResponse, error) {
	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 0, 1<<20), 8<<20)

	var dataLines []string
	flush := func() (jsonrpcResponse, bool, error) {
		if len(dataLines) == 0 {
			return jsonrpcResponse{}, false, nil
		}
		payload := strings.Join(dataLines, "\n")
		dataLines = dataLines[:0]
		var resp jsonrpcResponse
		if err := json.Unmarshal([]byte(payload), &resp); err != nil {
			// Not a JSON-RPC message we understand; skip it.
			return jsonrpcResponse{}, false, nil
		}
		if resp.ID != nil && *resp.ID == id {
			return resp, true, nil
		}
		return jsonrpcResponse{}, false, nil
	}

	for scanner.Scan() {
		line := scanner.Text()
		if line == "" { // event boundary
			if resp, ok, err := flush(); err != nil {
				return jsonrpcResponse{}, err
			} else if ok {
				return resp, nil
			}
			continue
		}
		if strings.HasPrefix(line, "data:") {
			dataLines = append(dataLines, strings.TrimPrefix(strings.TrimPrefix(line, "data:"), " "))
		}
		// Other SSE fields (event:, id:, retry:) are not needed here.
	}
	if err := scanner.Err(); err != nil {
		return jsonrpcResponse{}, err
	}
	// Stream ended; flush any trailing event.
	if resp, ok, err := flush(); err != nil {
		return jsonrpcResponse{}, err
	} else if ok {
		return resp, nil
	}
	return jsonrpcResponse{}, fmt.Errorf("event stream ended without a response for id %d", id)
}
