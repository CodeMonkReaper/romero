package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"

	"github.com/romero-project/romero/internal/application/capture"
	contextbuilder "github.com/romero-project/romero/internal/application/context"
	"github.com/romero-project/romero/internal/application/feedback"
	"github.com/romero-project/romero/internal/application/retrieval"
	"github.com/romero-project/romero/internal/domain/identity"
)

// Authenticator validates raw API keys into an authenticated Zero-Trust AuthContext.
type Authenticator interface {
	Authenticate(ctx context.Context, rawAPIKey string) (identity.AuthContext, error)
}

// CaptureService defines memory capture operations exposed via MCP.
type CaptureService interface {
	CaptureMemory(ctx context.Context, auth identity.AuthContext, cmd capture.CaptureCommand) (*capture.CaptureResult, error)
}

// RetrievalService defines memory search and single/batch retrieval exposed via MCP.
type RetrievalService interface {
	SearchMemories(ctx context.Context, auth identity.AuthContext, q retrieval.SearchQuery) (*retrieval.SearchResult, error)
	GetMemory(ctx context.Context, auth identity.AuthContext, q retrieval.GetMemoryQuery) (*retrieval.MemoryDetail, error)
}

// ContextService defines token-budgeted XML context assembly exposed via MCP.
type ContextService interface {
	BuildContext(ctx context.Context, auth identity.AuthContext, req contextbuilder.BuildContextRequest) (*contextbuilder.BuildContextResult, error)
}

// FeedbackService defines Bayesian feedback operations exposed via MCP.
type FeedbackService interface {
	RecordFeedback(ctx context.Context, auth identity.AuthContext, cmd feedback.FeedbackCommand) (*feedback.FeedbackResult, error)
}

// GovernorPreemptor marks active foreground agent requests for PowerGovernor preemption.
type GovernorPreemptor interface {
	BeginAgentRequest() func()
}

// Dependencies holds the domain/application services wired into the MCP server.
type Dependencies struct {
	Authenticator Authenticator
	Capture       CaptureService
	Retrieval     RetrievalService
	Context       ContextService
	Feedback      FeedbackService
	Governor      GovernorPreemptor
}

// Server dispatches JSON-RPC 2.0 MCP requests directly against ROMERO application services.
type Server struct {
	deps Dependencies
}

// NewServer constructs an MCP JSON-RPC 2.0 dispatcher with the given dependencies.
func NewServer(deps Dependencies) (*Server, error) {
	return &Server{deps: deps}, nil
}

// HandleRequest processes a single JSON-RPC 2.0 payload and returns the serialized response if one is required.
func (s *Server) HandleRequest(ctx context.Context, auth identity.AuthContext, raw []byte) ([]byte, bool, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		return marshalRPCError(nil, errCodeInvalidRequest, "empty request")
	}

	var req Request
	if err := json.Unmarshal(trimmed, &req); err != nil {
		return marshalRPCError(nil, errCodeParse, "parse error")
	}
	if req.JSONRPC != jsonRPCVersion || req.Method == "" {
		return marshalRPCError(req.ID, errCodeInvalidRequest, "invalid json-rpc request")
	}

	isNotification := len(bytes.TrimSpace(req.ID)) == 0 || bytes.Equal(bytes.TrimSpace(req.ID), []byte("null"))

	switch req.Method {
	case "initialize":
		if isNotification {
			return nil, false, nil
		}
		return marshalRPCResult(req.ID, InitializeResult{
			ProtocolVersion: mcpProtocolVersion,
			Capabilities: ServerCapabilities{
				Tools: &ToolsCapability{},
			},
			ServerInfo: ImplementationInfo{
				Name:    serverName,
				Version: serverVersion,
			},
		})
	case "notifications/initialized":
		return nil, false, nil
	case "ping":
		if isNotification {
			return nil, false, nil
		}
		return marshalRPCResult(req.ID, struct{}{})
	case "tools/list":
		if isNotification {
			return nil, false, nil
		}
		return marshalRPCResult(req.ID, ListToolsResult{Tools: canonicalTools})
	case "tools/call":
		var params CallToolParams
		if err := json.Unmarshal(req.Params, &params); err != nil || params.Name == "" {
			if isNotification {
				return nil, false, nil
			}
			return marshalRPCError(req.ID, errCodeInvalidParams, "invalid tools/call params")
		}
		if s.deps.Governor != nil {
			done := s.deps.Governor.BeginAgentRequest()
			defer done()
		}
		res := s.callTool(ctx, auth, params.Name, params.Arguments)
		if isNotification {
			return nil, false, nil
		}
		return marshalRPCResult(req.ID, res)
	default:
		if isNotification {
			return nil, false, nil
		}
		return marshalRPCError(req.ID, errCodeMethodNotFound, fmt.Sprintf("method %q not found", req.Method))
	}
}

func marshalRPCResult(id json.RawMessage, result any) ([]byte, bool, error) {
	if len(bytes.TrimSpace(id)) == 0 {
		id = json.RawMessage("null")
	}
	b, err := json.Marshal(Response{
		JSONRPC: jsonRPCVersion,
		ID:      id,
		Result:  result,
	})
	if err != nil {
		return nil, false, err
	}
	return b, true, nil
}

func marshalRPCError(id json.RawMessage, code int, message string) ([]byte, bool, error) {
	if len(bytes.TrimSpace(id)) == 0 {
		id = json.RawMessage("null")
	}
	b, err := json.Marshal(Response{
		JSONRPC: jsonRPCVersion,
		ID:      id,
		Error: &RPCError{
			Code:    code,
			Message: message,
		},
	})
	if err != nil {
		return nil, false, err
	}
	return b, true, nil
}
