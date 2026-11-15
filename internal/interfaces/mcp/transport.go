package mcp

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/romero-project/romero/internal/domain/identity"
)

const (
	maxMCPMessageBytes = 4 << 20
	sseKeepAlivePeriod = 15 * time.Second
)

// ServeStdio authenticates rawAPIKey once at startup and processes newline-delimited JSON-RPC 2.0 requests.
func (s *Server) ServeStdio(ctx context.Context, r io.Reader, w io.Writer, rawAPIKey string) error {
	if s.deps.Authenticator == nil {
		return errors.New("mcp stdio: authenticator is required")
	}
	rawAPIKey = strings.TrimSpace(rawAPIKey)
	if rawAPIKey == "" {
		return errors.New("mcp stdio: api key is required")
	}
	auth, err := s.deps.Authenticator.Authenticate(ctx, rawAPIKey)
	if err != nil {
		return fmt.Errorf("mcp stdio: authentication failed: %w", err)
	}

	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), maxMCPMessageBytes)

	var writeMu sync.Mutex
	for scanner.Scan() {
		if ctx.Err() != nil {
			return nil
		}
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 {
			continue
		}
		resp, hasResp, handleErr := s.HandleRequest(ctx, auth, line)
		if handleErr != nil {
			return fmt.Errorf("mcp stdio: handle request: %w", handleErr)
		}
		if !hasResp {
			continue
		}
		writeMu.Lock()
		_, wErr := w.Write(append(resp, '\n'))
		writeMu.Unlock()
		if wErr != nil {
			return fmt.Errorf("mcp stdio: write response: %w", wErr)
		}
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("mcp stdio: read input: %w", err)
	}
	return nil
}

type sseSession struct {
	tenantID    uuid.UUID
	principalID uuid.UUID
	ch          chan []byte
}

// HTTPHandler returns an authenticated net/http handler exposing GET /sse, POST /message, and POST /mcp.
func (s *Server) HTTPHandler() http.Handler {
	var mu sync.RWMutex
	sessions := make(map[string]*sseSession)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /sse", func(w http.ResponseWriter, r *http.Request) {
		auth, ok := s.authenticateHTTP(w, r)
		if !ok {
			return
		}
		flusher, ok := w.(http.Flusher)
		if !ok {
			writeHTTPError(w, http.StatusInternalServerError, "streaming unsupported")
			return
		}
		sessionID, err := newSessionID()
		if err != nil {
			writeHTTPError(w, http.StatusInternalServerError, "failed to allocate session")
			return
		}
		sess := &sseSession{
			tenantID:    auth.TenantID,
			principalID: auth.PrincipalID,
			ch:          make(chan []byte, 64),
		}
		mu.Lock()
		sessions[sessionID] = sess
		mu.Unlock()
		defer func() {
			mu.Lock()
			delete(sessions, sessionID)
			mu.Unlock()
		}()

		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		w.WriteHeader(http.StatusOK)

		_, _ = fmt.Fprintf(w, "event: endpoint\ndata: /message?sessionId=%s\n\n", sessionID)
		flusher.Flush()

		ticker := time.NewTicker(sseKeepAlivePeriod)
		defer ticker.Stop()

		for {
			select {
			case <-r.Context().Done():
				return
			case msg := <-sess.ch:
				_, _ = fmt.Fprintf(w, "event: message\ndata: %s\n\n", msg)
				flusher.Flush()
			case <-ticker.C:
				_, _ = io.WriteString(w, ": keepalive\n\n")
				flusher.Flush()
			}
		}
	})

	mux.HandleFunc("POST /message", func(w http.ResponseWriter, r *http.Request) {
		auth, ok := s.authenticateHTTP(w, r)
		if !ok {
			return
		}
		sessionID := strings.TrimSpace(r.URL.Query().Get("sessionId"))
		if sessionID == "" {
			writeHTTPError(w, http.StatusBadRequest, "missing sessionId")
			return
		}
		mu.RLock()
		sess, exists := sessions[sessionID]
		mu.RUnlock()
		if !exists {
			writeHTTPError(w, http.StatusNotFound, "session not found")
			return
		}
		if sess.tenantID != auth.TenantID || sess.principalID != auth.PrincipalID {
			writeHTTPError(w, http.StatusForbidden, "session belongs to another principal")
			return
		}

		body, ok := readBoundedBody(w, r)
		if !ok {
			return
		}
		resp, hasResp, err := s.HandleRequest(r.Context(), auth, body)
		if err != nil {
			writeHTTPError(w, http.StatusInternalServerError, "failed to process request")
			return
		}
		if hasResp {
			select {
			case sess.ch <- resp:
			case <-r.Context().Done():
				return
			default:
				writeHTTPError(w, http.StatusServiceUnavailable, "sse session buffer full")
				return
			}
		}
		w.WriteHeader(http.StatusAccepted)
	})

	mux.HandleFunc("POST /mcp", func(w http.ResponseWriter, r *http.Request) {
		auth, ok := s.authenticateHTTP(w, r)
		if !ok {
			return
		}
		body, ok := readBoundedBody(w, r)
		if !ok {
			return
		}
		resp, hasResp, err := s.HandleRequest(r.Context(), auth, body)
		if err != nil {
			writeHTTPError(w, http.StatusInternalServerError, "failed to process request")
			return
		}
		if !hasResp {
			w.WriteHeader(http.StatusAccepted)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(resp)
	})

	return mux
}

func (s *Server) authenticateHTTP(w http.ResponseWriter, r *http.Request) (identity.AuthContext, bool) {
	if s.deps.Authenticator == nil {
		writeHTTPError(w, http.StatusUnauthorized, "missing or invalid credentials")
		return identity.AuthContext{}, false
	}
	rawKey := extractHTTPAPIKey(r)
	if rawKey == "" {
		writeHTTPError(w, http.StatusUnauthorized, "missing or invalid credentials")
		return identity.AuthContext{}, false
	}
	auth, err := s.deps.Authenticator.Authenticate(r.Context(), rawKey)
	if err != nil {
		writeHTTPError(w, http.StatusUnauthorized, "missing or invalid credentials")
		return identity.AuthContext{}, false
	}
	return auth, true
}

func extractHTTPAPIKey(r *http.Request) string {
	if authz := strings.TrimSpace(r.Header.Get("Authorization")); authz != "" {
		if len(authz) > 7 && strings.EqualFold(authz[:7], "Bearer ") {
			return strings.TrimSpace(authz[7:])
		}
		return ""
	}
	return strings.TrimSpace(r.Header.Get("X-API-Key"))
}

func readBoundedBody(w http.ResponseWriter, r *http.Request) ([]byte, bool) {
	body, err := io.ReadAll(io.LimitReader(r.Body, maxMCPMessageBytes+1))
	if err != nil {
		writeHTTPError(w, http.StatusBadRequest, "failed to read request body")
		return nil, false
	}
	if len(body) > maxMCPMessageBytes {
		writeHTTPError(w, http.StatusRequestEntityTooLarge, "request body too large")
		return nil, false
	}
	return body, true
}

func writeHTTPError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"error":  msg,
		"status": status,
	})
}

func newSessionID() (string, error) {
	var buf [16]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf[:]), nil
}
