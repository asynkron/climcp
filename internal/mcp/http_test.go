package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/asynkron/climcp/internal/config"
)

// TestHTTPTransport exercises the full handshake + tools/list + tools/call over
// the Streamable HTTP transport, with the tools/call answer delivered as SSE.
func TestHTTPTransport(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req struct {
			ID     *int   `json:"id"`
			Method string `json:"method"`
		}
		json.Unmarshal(body, &req)

		writeJSON := func(result interface{}) {
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("Mcp-Session-Id", "sess-123")
			json.NewEncoder(w).Encode(map[string]interface{}{
				"jsonrpc": "2.0", "id": req.ID, "result": result,
			})
		}

		switch req.Method {
		case "initialize":
			writeJSON(map[string]interface{}{
				"protocolVersion": "2025-06-18",
				"serverInfo":      map[string]interface{}{"name": "httpmock", "version": "9.9"},
			})
		case "notifications/initialized":
			w.WriteHeader(http.StatusAccepted)
		case "tools/list":
			writeJSON(map[string]interface{}{
				"tools": []map[string]interface{}{
					{"name": "ping", "description": "pong"},
				},
			})
		case "tools/call":
			// Verify the session id round-tripped from initialize.
			if got := r.Header.Get("Mcp-Session-Id"); got != "sess-123" {
				t.Errorf("missing session id, got %q", got)
			}
			// Answer as an SSE stream.
			w.Header().Set("Content-Type", "text/event-stream")
			payload, _ := json.Marshal(map[string]interface{}{
				"jsonrpc": "2.0", "id": req.ID,
				"result": map[string]interface{}{
					"content": []map[string]interface{}{{"type": "text", "text": "pong!"}},
				},
			})
			fmt.Fprintf(w, "event: message\ndata: %s\n\n", payload)
		default:
			writeJSON(map[string]interface{}{})
		}
	}))
	defer srv.Close()

	client, err := Connect(context.Background(), config.Server{Type: "http", URL: srv.URL})
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer client.Close()

	if info := client.ServerInfo(); info.Name != "httpmock" {
		t.Fatalf("serverInfo.Name = %q, want httpmock", info.Name)
	}

	tools, err := client.ListTools()
	if err != nil {
		t.Fatalf("list tools: %v", err)
	}
	if len(tools) != 1 || tools[0].Name != "ping" {
		t.Fatalf("tools = %#v", tools)
	}

	res, err := client.CallTool("ping", map[string]interface{}{})
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	if len(res.Content) != 1 || res.Content[0].Text != "pong!" {
		t.Fatalf("result content = %#v", res.Content)
	}
}
