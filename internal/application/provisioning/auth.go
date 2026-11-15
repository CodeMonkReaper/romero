package provisioning

import (
	"context"
	"crypto/subtle"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/romero-project/romero/internal/domain/identity"
	"github.com/romero-project/romero/internal/domain/memory"
)

// AuthOption configures optional dependencies on Authenticator.
type AuthOption func(*Authenticator)

// WithAuthClock injects a deterministic clock for credential expiration checks.
func WithAuthClock(nowFn func() time.Time) AuthOption {
	return func(a *Authenticator) {
		if nowFn != nil {
			a.nowFn = nowFn
		}
	}
}

// Authenticator verifies raw API keys via prefix index lookup and constant-time SHA-256 matching.
type Authenticator struct {
	lookup PrefixLookup
	audit  memory.AuditLogger
	nowFn  func() time.Time
}

// NewAuthenticator builds a SHA-256 Zero-Trust Authenticator.
func NewAuthenticator(lookup PrefixLookup, audit memory.AuditLogger, opts ...AuthOption) *Authenticator {
	a := &Authenticator{
		lookup: lookup,
		audit:  audit,
		nowFn:  func() time.Time { return time.Now().UTC() },
	}
	for _, opt := range opts {
		if opt != nil {
			opt(a)
		}
	}
	return a
}

// Authenticate validates rawAPIKey in constant time and constructs identity.AuthContext.
func (a *Authenticator) Authenticate(ctx context.Context, rawAPIKey string) (identity.AuthContext, error) {
	if a == nil || a.lookup == nil {
		return identity.AuthContext{}, fmt.Errorf("%w: authenticator uninitialized", ErrInvalidInput)
	}

	prefix, digestHex, ok := ParseAPIKey(rawAPIKey)
	if !ok {
		if err := a.recordDenied(ctx, nil, nil, nil, "malformed_api_key"); err != nil {
			return identity.AuthContext{}, err
		}
		return identity.AuthContext{}, ErrUnauthenticated
	}

	candidates, err := a.lookup.LookupPrincipalsByPrefix(ctx, prefix)
	if err != nil {
		return identity.AuthContext{}, err
	}

	var matched *PrincipalAuthCandidate
	gotBytes := []byte(digestHex)
	for i := range candidates {
		cand := &candidates[i]
		storedHex := strings.ToLower(strings.TrimSpace(cand.APIKeySHA256))
		if len(storedHex) == len(gotBytes) && subtle.ConstantTimeCompare(gotBytes, []byte(storedHex)) == 1 && matched == nil {
			matched = cand
		}
	}

	if matched == nil {
		if err := a.recordDenied(ctx, nil, nil, nil, "invalid_api_key"); err != nil {
			return identity.AuthContext{}, err
		}
		return identity.AuthContext{}, ErrUnauthenticated
	}

	now := a.nowFn()
	pr := matched.Principal
	if !validPrincipalType(pr.PrincipalType) || (pr.PrincipalType == identity.PrincipalAgent && (pr.AgentID == nil || *pr.AgentID == uuid.Nil)) {
		tid := pr.TenantID
		pid := pr.ID
		if err := a.recordDenied(ctx, &tid, &pid, pr.AgentID, "invalid_principal_state"); err != nil {
			return identity.AuthContext{}, err
		}
		return identity.AuthContext{}, ErrUnauthenticated
	}
	if !matched.TenantActive || !pr.IsActive || !matched.AgentActive || (pr.ExpiresAt != nil && !pr.ExpiresAt.After(now)) {
		tid := pr.TenantID
		pid := pr.ID
		if err := a.recordDenied(ctx, &tid, &pid, pr.AgentID, "principal_inactive_or_expired"); err != nil {
			return identity.AuthContext{}, err
		}
		return identity.AuthContext{}, ErrUnauthenticated
	}

	return identity.AuthContext{
		TenantID:        pr.TenantID,
		PrincipalID:     pr.ID,
		PrincipalType:   pr.PrincipalType,
		AgentID:         copyUUIDPtr(pr.AgentID),
		AllowedProjects: append([]uuid.UUID(nil), pr.AllowedProjects...),
		AllowedScopes:   append([]identity.ScopeType(nil), pr.AllowedScopes...),
		IsAdmin:         pr.IsAdmin && pr.PrincipalType != identity.PrincipalAgent,
	}, nil
}

func (a *Authenticator) recordDenied(ctx context.Context, tenantID, principalID, agentID *uuid.UUID, reason string) error {
	if a.audit == nil {
		return fmt.Errorf("%w: audit logger is nil", ErrAuditUnavailable)
	}
	ev := memory.AuditEvent{
		ID:           uuid.New(),
		TenantID:     tenantID,
		PrincipalID:  principalID,
		AgentID:      agentID,
		Operation:    "authenticate",
		ResourceType: "principal",
		ResourceID:   principalID,
		Outcome:      memory.AuditDenied,
		Details:      map[string]any{"reason": reason},
		CreatedAt:    a.nowFn(),
	}
	if err := a.audit.RecordDenied(ctx, ev); err != nil {
		return fmt.Errorf("%w: %v", ErrAuditUnavailable, err)
	}
	return nil
}

func copyUUIDPtr(id *uuid.UUID) *uuid.UUID {
	if id == nil {
		return nil
	}
	v := *id
	return &v
}
