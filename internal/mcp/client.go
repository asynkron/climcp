// Package mcp implements a minimal Model Context Protocol client. It speaks
// JSON-RPC 2.0 over a pluggable transport: stdio (a spawned server process) or
// HTTP (the Streamable HTTP transport, which also covers SSE responses).
package mcp

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/asynkron/climcp/internal/config"
)

// protocolVersion is the MCP revision we advertise during initialize.
const protocolVersion = "2025-06-18"

// transport carries JSON-RPC messages to and from a server.
type transport interface {
	// roundTrip sends a request bearing the given id and returns the matching
	// response envelope.
	roundTrip(ctx context.Context, id int, message []byte) (jsonrpcResponse, error)
	// send delivers a notification (no response expected).
	send(ctx context.Context, message []byte) error
	// close releases the transport's resources.
	close() error
}

// Client is a connected MCP server. Call Close when done.
type Client struct {
	ctx       context.Context
	transport transport
	nextID    int

	serverInfo Implementation
}

// Implementation mirrors the MCP clientInfo/serverInfo object.
type Implementation struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// Tool is one entry returned by tools/list.
type Tool struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	InputSchema json.RawMessage `json:"inputSchema,omitempty"`
}

type jsonrpcRequest struct {
	JSONRPC string      `json:"jsonrpc"`
	ID      int         `json:"id"`
	Method  string      `json:"method"`
	Params  interface{} `json:"params,omitempty"`
}

type jsonrpcNotification struct {
	JSONRPC string      `json:"jsonrpc"`
	Method  string      `json:"method"`
	Params  interface{} `json:"params,omitempty"`
}

type jsonrpcError struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

func (e *jsonrpcError) Error() string {
	return fmt.Sprintf("server error %d: %s", e.Code, e.Message)
}

type jsonrpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      *int            `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *jsonrpcError   `json:"error,omitempty"`
	Method  string          `json:"method,omitempty"` // set on server-initiated notifications
}

// Connect chooses a transport from the server config, opens it, and performs
// the MCP initialize handshake.
func Connect(ctx context.Context, srv config.Server) (*Client, error) {
	var (
		t   transport
		err error
	)
	switch srv.Transport() {
	case "http":
		t, err = newHTTPTransport(srv)
	default:
		t, err = newStdioTransport(ctx, srv)
	}
	if err != nil {
		return nil, err
	}

	c := &Client{ctx: ctx, transport: t}
	if err := c.initialize(); err != nil {
		c.Close()
		return nil, err
	}
	return c, nil
}

// Close shuts down the transport.
func (c *Client) Close() error {
	if c.transport != nil {
		return c.transport.close()
	}
	return nil
}

// ServerInfo returns the implementation info reported during initialize.
func (c *Client) ServerInfo() Implementation { return c.serverInfo }

func (c *Client) initialize() error {
	params := map[string]interface{}{
		"protocolVersion": protocolVersion,
		"capabilities":    map[string]interface{}{},
		"clientInfo": Implementation{
			Name:    "climcp",
			Version: "0.1.0",
		},
	}

	var result struct {
		ProtocolVersion string         `json:"protocolVersion"`
		ServerInfo      Implementation `json:"serverInfo"`
	}
	if err := c.call("initialize", params, &result); err != nil {
		return fmt.Errorf("initialize handshake failed: %w", err)
	}
	c.serverInfo = result.ServerInfo

	if err := c.notify("notifications/initialized", map[string]interface{}{}); err != nil {
		return fmt.Errorf("sending initialized notification: %w", err)
	}
	return nil
}

// ListTools returns the tools the server exposes, following pagination.
func (c *Client) ListTools() ([]Tool, error) {
	var all []Tool
	cursor := ""
	for {
		params := map[string]interface{}{}
		if cursor != "" {
			params["cursor"] = cursor
		}
		var result struct {
			Tools      []Tool `json:"tools"`
			NextCursor string `json:"nextCursor,omitempty"`
		}
		if err := c.call("tools/list", params, &result); err != nil {
			return nil, err
		}
		all = append(all, result.Tools...)
		if result.NextCursor == "" {
			break
		}
		cursor = result.NextCursor
	}
	return all, nil
}

// CallToolResult is the (simplified) result of tools/call.
type CallToolResult struct {
	Content           []ContentBlock  `json:"content"`
	IsError           bool            `json:"isError,omitempty"`
	StructuredContent json.RawMessage `json:"structuredContent,omitempty"`
}

// ContentBlock is one piece of tool output.
type ContentBlock struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
	// Raw keeps the full block so non-text types (image, resource, ...) survive.
	Raw json.RawMessage `json:"-"`
}

func (b *ContentBlock) UnmarshalJSON(data []byte) error {
	type alias ContentBlock
	var a alias
	if err := json.Unmarshal(data, &a); err != nil {
		return err
	}
	*b = ContentBlock(a)
	b.Raw = append(json.RawMessage(nil), data...)
	return nil
}

// CallTool invokes a tool with the given arguments.
func (c *Client) CallTool(name string, arguments map[string]interface{}) (*CallToolResult, error) {
	params := map[string]interface{}{
		"name":      name,
		"arguments": arguments,
	}
	var result CallToolResult
	if err := c.call("tools/call", params, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// call performs a synchronous JSON-RPC request, decoding the result into out.
func (c *Client) call(method string, params interface{}, out interface{}) error {
	c.nextID++
	id := c.nextID

	data, err := json.Marshal(jsonrpcRequest{JSONRPC: "2.0", ID: id, Method: method, Params: params})
	if err != nil {
		return err
	}

	resp, err := c.transport.roundTrip(c.ctx, id, data)
	if err != nil {
		return err
	}
	if resp.Error != nil {
		return resp.Error
	}
	if out == nil || len(resp.Result) == 0 {
		return nil
	}
	return json.Unmarshal(resp.Result, out)
}

func (c *Client) notify(method string, params interface{}) error {
	data, err := json.Marshal(jsonrpcNotification{JSONRPC: "2.0", Method: method, Params: params})
	if err != nil {
		return err
	}
	return c.transport.send(c.ctx, data)
}
