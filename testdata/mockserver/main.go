// Command mockserver is a tiny stdio MCP server used to exercise climcp
// end-to-end. It implements just enough of the protocol: initialize,
// tools/list, and tools/call for a single "echo" tool.
package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
)

type request struct {
	ID     *json.RawMessage `json:"id"`
	Method string           `json:"method"`
	Params json.RawMessage  `json:"params"`
}

func main() {
	in := bufio.NewScanner(os.Stdin)
	in.Buffer(make([]byte, 0, 1<<20), 1<<20)
	out := bufio.NewWriter(os.Stdout)
	defer out.Flush()

	send := func(id *json.RawMessage, result interface{}) {
		resp := map[string]interface{}{"jsonrpc": "2.0", "id": id, "result": result}
		b, _ := json.Marshal(resp)
		out.Write(b)
		out.WriteByte('\n')
		out.Flush()
	}

	for in.Scan() {
		var req request
		if err := json.Unmarshal(in.Bytes(), &req); err != nil {
			continue
		}
		switch req.Method {
		case "initialize":
			send(req.ID, map[string]interface{}{
				"protocolVersion": "2025-06-18",
				"capabilities":    map[string]interface{}{"tools": map[string]interface{}{}},
				"serverInfo":      map[string]interface{}{"name": "mockserver", "version": "1.0.0"},
			})
		case "notifications/initialized":
			// no response to notifications
		case "tools/list":
			send(req.ID, map[string]interface{}{
				"tools": []map[string]interface{}{
					{
						"name":        "echo",
						"description": "Echoes the message back.",
						"inputSchema": map[string]interface{}{
							"type": "object",
							"properties": map[string]interface{}{
								"message": map[string]interface{}{"type": "string"},
								"count":   map[string]interface{}{"type": "integer"},
							},
							"required": []string{"message"},
						},
					},
				},
			})
		case "tools/call":
			var p struct {
				Name      string                 `json:"name"`
				Arguments map[string]interface{} `json:"arguments"`
			}
			json.Unmarshal(req.Params, &p)
			text := fmt.Sprintf("echo: %v (count=%v)", p.Arguments["message"], p.Arguments["count"])
			send(req.ID, map[string]interface{}{
				"content": []map[string]interface{}{
					{"type": "text", "text": text},
				},
			})
		default:
			send(req.ID, map[string]interface{}{})
		}
	}
}
