package contextbuilder_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	contextbuilder "github.com/romero-project/romero/internal/application/context"
	"github.com/romero-project/romero/internal/application/retrieval"
	"github.com/romero-project/romero/internal/domain/identity"
	"github.com/romero-project/romero/internal/domain/memory"
)

type fakeCandidateRetriever struct {
	items    []retrieval.ContextCandidate
	degraded bool
	err      error
}

func (f fakeCandidateRetriever) SearchForContext(_ context.Context, _ identity.AuthContext, _ retrieval.SearchQuery) ([]retrieval.ContextCandidate, bool, error) {
	return f.items, f.degraded, f.err
}

type fakeAuditLogger struct {
	allowed []memory.AuditEvent
	denied  []memory.AuditEvent
}

func (f *fakeAuditLogger) RecordAllowed(_ context.Context, ev memory.AuditEvent) error {
	f.allowed = append(f.allowed, ev)
	return nil
}
func (f *fakeAuditLogger) RecordDenied(_ context.Context, ev memory.AuditEvent) error {
	f.denied = append(f.denied, ev)
	return nil
}

func TestContextBuilder_BuildContext_SubmodularProgressiveDisclosureAndXML(t *testing.T) {
	t.Parallel()

	tenantID := uuid.New()
	projectID := uuid.New()
	auth := identity.AuthContext{
		TenantID: tenantID, PrincipalID: uuid.New(), PrincipalType: identity.PrincipalAgent,
		AllowedProjects: []uuid.UUID{projectID}, AllowedScopes: []identity.ScopeType{identity.ScopeProjectShared},
	}

	m1 := uuid.New()
	m2 := uuid.New()
	retriever := fakeCandidateRetriever{
		items: []retrieval.ContextCandidate{
			{
				ID: m1, Summary: "short summary 1", Content: "full detailed content 1 with extra tokens",
				TokenCount: 40, FinalScore: 0.9, DenseVec: []float32{1, 0},
				Type: memory.TypeSemantic, EpistemicStatus: memory.EpistemicVerified, Confidence: 0.95,
				PrimarySourceID: uuid.New(), HasConflictAlert: true,
			},
			{
				ID: m2, Summary: "compact summary 2", Content: strings.Repeat("very long redundant content ", 20),
				TokenCount: 120, FinalScore: 0.85, DenseVec: []float32{0, 1},
				Type: memory.TypeProcedural, EpistemicStatus: memory.EpistemicObserved, Confidence: 0.8,
				PrimarySourceID: uuid.New(),
			},
		},
	}
	audit := &fakeAuditLogger{}
	svc, err := contextbuilder.NewService(retriever, audit, func() time.Time { return time.Now().UTC() })
	if err != nil {
		t.Fatalf("NewService failed: %v", err)
	}

	res, err := svc.BuildContext(context.Background(), auth, contextbuilder.BuildContextRequest{
		Query:     "jwt rotation",
		MaxTokens: 80,
		Hops:      2,
	})
	if err != nil {
		t.Fatalf("BuildContext failed: %v", err)
	}
	if res.UsedTokens > res.MaxTokens {
		t.Fatalf("UsedTokens (%d) exceeded MaxTokens (%d)", res.UsedTokens, res.MaxTokens)
	}
	if !res.HasConflictAlert {
		t.Fatal("expected HasConflictAlert=true")
	}
	if len(res.Items) == 0 {
		t.Fatal("expected at least 1 selected context item")
	}
	if !strings.Contains(res.CombinedXML, `untrusted_content="true"`) || !strings.Contains(res.CombinedXML, "<romero_memory ") {
		t.Fatalf("expected untrusted XML envelope, got: %s", res.CombinedXML)
	}
	if len(audit.allowed) != 1 || audit.allowed[0].Operation != "build_context" {
		t.Fatalf("expected build_context audit event, got %+v", audit.allowed)
	}

	// Validation and constructor guards.
	if _, err := contextbuilder.NewService(nil, audit, nil); err == nil {
		t.Fatal("expected error for nil retriever")
	}
	if _, err := contextbuilder.NewService(retriever, nil, nil); err == nil {
		t.Fatal("expected error for nil audit")
	}
	if _, err := svc.BuildContext(context.Background(), identity.AuthContext{}, contextbuilder.BuildContextRequest{Query: "x", MaxTokens: 128}); !errors.Is(err, contextbuilder.ErrUnauthorized) {
		t.Fatalf("expected ErrUnauthorized, got %v", err)
	}
	if _, err := svc.BuildContext(context.Background(), auth, contextbuilder.BuildContextRequest{Query: "x", MaxTokens: 32}); !errors.Is(err, contextbuilder.ErrValidation) {
		t.Fatalf("expected ErrValidation for MaxTokens < 64, got %v", err)
	}
	if _, err := svc.BuildContext(context.Background(), auth, contextbuilder.BuildContextRequest{Query: "x", MaxTokens: 40000}); !errors.Is(err, contextbuilder.ErrValidation) {
		t.Fatalf("expected ErrValidation for MaxTokens > 32768, got %v", err)
	}

	// Compact summary fallback when full Content exceeds budget + DegradedMode propagation.
	compactSvc, _ := contextbuilder.NewService(fakeCandidateRetriever{
		degraded: true,
		items: []retrieval.ContextCandidate{
			{
				ID: uuid.New(), Summary: "compact only", Content: strings.Repeat("huge ", 200),
				TokenCount: 0, FinalScore: 0.9, Type: memory.TypeSemantic,
				EpistemicStatus: memory.EpistemicObserved, Confidence: 0.7, PrimarySourceID: uuid.New(),
			},
		},
	}, audit, nil)
	compactRes, err := compactSvc.BuildContext(context.Background(), auth, contextbuilder.BuildContextRequest{
		Query: "compact", MaxTokens: 64, Hops: 1,
	})
	if err != nil {
		t.Fatalf("BuildContext compact failed: %v", err)
	}
	if !compactRes.DegradedMode || len(compactRes.Items) != 1 || !compactRes.Items[0].IsCompact {
		t.Fatalf("expected DegradedMode=true and IsCompact=true, got %+v", compactRes)
	}
}
