package mcp_test

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/romero-project/romero/internal/domain/identity"
	"github.com/romero-project/romero/internal/interfaces/mcp"
)

type stubAuthenticator struct {
	keys map[string]identity.AuthContext
}

func (a stubAuthenticator) Authenticate(_ context.Context, rawKey string) (identity.AuthContext, error) {
	caller, ok := a.keys[rawKey]
	if !ok {
		return identity.AuthContext{}, errors.New("invalid api key")
	}
	return caller, nil
}

func TestServeStdio_AuthAndRoundTrip(t *testing.T) {
	caller := testCaller()
	srv, err := mcp.NewServer(mcp.Dependencies{
		Authenticator: stubAuthenticator{keys: map[string]identity.AuthContext{"valid-key": caller}},
	})
	if err != nil {
		t.Fatalf("NewServer failed: %v", err)
	}

	// Missing or invalid key must fail at startup
	var out bytes.Buffer
	if err := srv.ServeStdio(context.Background(), strings.NewReader(""), &out, ""); err == nil {
		t.Fatal("expected error for empty API key")
	}
	if err := srv.ServeStdio(context.Background(), strings.NewReader(""), &out, "bad-key"); err == nil {
		t.Fatal("expected error for invalid API key")
	}

	// Valid key processes initialize + tools/list + notification
	input := strings.Join([]string{
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`,
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/list"}`,
	}, "\n") + "\n"

	out.Reset()
	if err := srv.ServeStdio(context.Background(), strings.NewReader(input), &out, "valid-key"); err != nil {
		t.Fatalf("ServeStdio failed: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 JSON-RPC response lines (notification produces none), got %d: %s", len(lines), out.String())
	}
	if !strings.Contains(lines[0], `"romero-mcp"`) || !strings.Contains(lines[1], `"romero_memory_get"`) {
		t.Fatalf("unexpected stdio output: %v", lines)
	}
}

func TestHTTPHandler_AuthPostMCPAndSSEOwnership(t *testing.T) {
	callerA := testCaller()
	callerB := testCaller()
	srv, err := mcp.NewServer(mcp.Dependencies{
		Authenticator: stubAuthenticator{keys: map[string]identity.AuthContext{
			"key-a": callerA,
			"key-b": callerB,
		}},
	})
	if err != nil {
		t.Fatalf("NewServer failed: %v", err)
	}

	ts := httptest.NewServer(srv.HTTPHandler())
	defer ts.Close()

	// 1. Unauthenticated POST /mcp -> 401
	resp, err := http.Post(ts.URL+"/mcp", "application/json", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"ping"}`))
	if err != nil {
		t.Fatalf("POST /mcp failed: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401 on unauthenticated POST /mcp, got %d", resp.StatusCode)
	}

	// 2. Authenticated POST /mcp -> 200 OK
	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`))
	req.Header.Set("Authorization", "Bearer key-a")
	resp, err = ts.Client().Do(req)
	if err != nil {
		t.Fatalf("authenticated POST /mcp failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 on POST /mcp, got %d", resp.StatusCode)
	}
	var rpcResp struct {
		Result struct {
			Tools []json.RawMessage `json:"tools"`
		} `json:"result"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&rpcResp); err != nil || len(rpcResp.Result.Tools) != 5 {
		t.Fatalf("unexpected POST /mcp response: err=%v tools=%d", err, len(rpcResp.Result.Tools))
	}

	// 3. SSE handshake as key-a + cross-principal POST /message rejection (403) + valid POST /message (202)
	sseCtx, cancelSSE := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancelSSE()
	sseReq, _ := http.NewRequestWithContext(sseCtx, http.MethodGet, ts.URL+"/sse", nil)
	sseReq.Header.Set("X-API-Key", "key-a")
	sseResp, err := ts.Client().Do(sseReq)
	if err != nil {
		t.Fatalf("GET /sse failed: %v", err)
	}
	defer sseResp.Body.Close()
	if sseResp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 on GET /sse, got %d", sseResp.StatusCode)
	}

	scanner := bufio.NewScanner(sseResp.Body)
	var endpointPath string
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "data: ") {
			endpointPath = strings.TrimPrefix(line, "data: ")
			break
		}
	}
	if !strings.HasPrefix(endpointPath, "/message?sessionId=") {
		t.Fatalf("expected /message?sessionId= endpoint event, got %q", endpointPath)
	}

	// Cross-principal callerB must receive 403 Forbidden
	crossReq, _ := http.NewRequest(http.MethodPost, ts.URL+endpointPath, strings.NewReader(`{"jsonrpc":"2.0","id":9,"method":"ping"}`))
	crossReq.Header.Set("Authorization", "Bearer key-b")
	crossResp, err := ts.Client().Do(crossReq)
	if err != nil {
		t.Fatalf("cross POST /message failed: %v", err)
	}
	crossResp.Body.Close()
	if crossResp.StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403 for different principal on /message, got %d", crossResp.StatusCode)
	}

	// Same principal callerA receives 202 Accepted and JSON-RPC response arrives on SSE stream
	msgReq, _ := http.NewRequest(http.MethodPost, ts.URL+endpointPath, strings.NewReader(`{"jsonrpc":"2.0","id":10,"method":"ping"}`))
	msgReq.Header.Set("Authorization", "Bearer key-a")
	msgResp, err := ts.Client().Do(msgReq)
	if err != nil {
		t.Fatalf("POST /message failed: %v", err)
	}
	msgResp.Body.Close()
	if msgResp.StatusCode != http.StatusAccepted {
		t.Fatalf("expected 202 Accepted on POST /message, got %d", msgResp.StatusCode)
	}

	var sseMsg string
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "data: ") {
			sseMsg = strings.TrimPrefix(line, "data: ")
			break
		}
	}
	if !strings.Contains(sseMsg, `"id":10`) || !strings.Contains(sseMsg, `"result":{}`) {
		t.Fatalf("expected ping response over SSE, got %q", sseMsg)
	}

	// 4. Unauthenticated GET /sse and POST /message -> 401
	unauthSSE, err := http.Get(ts.URL + "/sse")
	if err != nil {
		t.Fatalf("unauth GET /sse failed: %v", err)
	}
	unauthSSE.Body.Close()
	if unauthSSE.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401 on unauth GET /sse, got %d", unauthSSE.StatusCode)
	}

	unauthMsg, err := http.Post(ts.URL+endpointPath, "application/json", strings.NewReader(`{"jsonrpc":"2.0","id":11,"method":"ping"}`))
	if err != nil {
		t.Fatalf("unauth POST /message failed: %v", err)
	}
	unauthMsg.Body.Close()
	if unauthMsg.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401 on unauth POST /message, got %d", unauthMsg.StatusCode)
	}

	// 5. Notification on POST /mcp -> 202 Accepted
	notifReq, _ := http.NewRequest(http.MethodPost, ts.URL+"/mcp", strings.NewReader(`{"jsonrpc":"2.0","method":"notifications/initialized"}`))
	notifReq.Header.Set("Authorization", "Bearer key-a")
	notifResp, err := ts.Client().Do(notifReq)
	if err != nil {
		t.Fatalf("notification POST /mcp failed: %v", err)
	}
	notifResp.Body.Close()
	if notifResp.StatusCode != http.StatusAccepted {
		t.Fatalf("expected 202 on notification POST /mcp, got %d", notifResp.StatusCode)
	}

	// 6. Oversized > 4 MiB body on POST /mcp -> 413 Request Entity Too Large
	bigBody := strings.Repeat("a", (4<<20)+16)
	bigReq, _ := http.NewRequest(http.MethodPost, ts.URL+"/mcp", strings.NewReader(bigBody))
	bigReq.Header.Set("Authorization", "Bearer key-a")
	bigResp, err := ts.Client().Do(bigReq)
	if err != nil {
		t.Fatalf("oversized POST /mcp failed: %v", err)
	}
	bigResp.Body.Close()
	if bigResp.StatusCode != http.StatusRequestEntityTooLarge {
		t.Fatalf("expected 413 on oversized POST /mcp, got %d", bigResp.StatusCode)
	}
	_ = uuid.Nil
}

func TestServeStdio_LargePayloadBuffer(t *testing.T) {
	caller := testCaller()
	srv, err := mcp.NewServer(mcp.Dependencies{
		Authenticator: stubAuthenticator{keys: map[string]identity.AuthContext{"valid-key": caller}},
	})
	if err != nil {
		t.Fatalf("NewServer failed: %v", err)
	}
	// > 64 KiB (default bufio.Scanner limit) but < 4 MiB must succeed
	largePad := strings.Repeat("x", 100*1024)
	req := `{"jsonrpc":"2.0","id":99,"method":"ping","params":{"pad":"` + largePad + `"}}` + "\n"
	var out bytes.Buffer
	if err := srv.ServeStdio(context.Background(), strings.NewReader(req), &out, "valid-key"); err != nil {
		t.Fatalf("ServeStdio large payload failed: %v", err)
	}
	if !strings.Contains(out.String(), `"id":99`) {
		t.Fatalf("expected id 99 response, got %s", out.String())
	}
}
