// Package mcp implements ROMERO's native Zero-Trust Model Context Protocol (MCP) server over JSON-RPC 2.0.
package mcp

import "encoding/json"

const (
	jsonRPCVersion     = "2.0"
	mcpProtocolVersion = "2024-11-05"
	serverName         = "romero-mcp"
	serverVersion      = "1.0.0"

	errCodeParse          = -32700
	errCodeInvalidRequest = -32600
	errCodeMethodNotFound = -32601
	errCodeInvalidParams  = -32602
	errCodeInternal       = -32603
)

// Request represents a JSON-RPC 2.0 request or notification envelope.
type Request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// Response represents a JSON-RPC 2.0 response envelope.
type Response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *RPCError       `json:"error,omitempty"`
}

// RPCError models a standard JSON-RPC 2.0 error object.
type RPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

// InitializeResult describes the MCP server capabilities returned during handshake.
type InitializeResult struct {
	ProtocolVersion string             `json:"protocolVersion"`
	Capabilities    ServerCapabilities `json:"capabilities"`
	ServerInfo      ImplementationInfo `json:"serverInfo"`
}

// ServerCapabilities declares supported MCP primitives.
type ServerCapabilities struct {
	Tools *ToolsCapability `json:"tools,omitempty"`
}

// ToolsCapability indicates the server supports tool discovery and invocation.
type ToolsCapability struct {
	ListChanged bool `json:"listChanged,omitempty"`
}

// ImplementationInfo identifies the MCP server name and version.
type ImplementationInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// ListToolsResult is the response payload for tools/list.
type ListToolsResult struct {
	Tools []ToolDefinition `json:"tools"`
}

// ToolDefinition describes one MCP tool and its JSON Schema input contract.
type ToolDefinition struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"inputSchema"`
}

// CallToolParams is the decoded params object for tools/call.
type CallToolParams struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

// CallToolResult is the response payload for tools/call.
type CallToolResult struct {
	Content           []TextContent `json:"content"`
	StructuredContent any           `json:"structuredContent,omitempty"`
	IsError           bool          `json:"isError,omitempty"`
}

// TextContent represents a single text content block inside an MCP tool result.
type TextContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}
