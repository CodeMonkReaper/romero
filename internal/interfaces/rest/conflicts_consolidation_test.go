package rest_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/google/uuid"

	"github.com/romero-project/romero/internal/application/conflicts"
	"github.com/romero-project/romero/internal/application/consolidation"
	"github.com/romero-project/romero/internal/domain/governor"
	"github.com/romero-project/romero/internal/domain/identity"
	"github.com/romero-project/romero/internal/domain/memory"
	"github.com/romero-project/romero/internal/interfaces/rest"
)

type fakeConflictSvc struct {
	listRes   []conflicts.ConflictRecordView
	reviewRes *conflicts.ConflictRecordView
	lastCmd   *conflicts.ReviewConflictCommand
	err       error
}

func (f fakeConflictSvc) ListConflicts(_ context.Context, _ identity.AuthContext, _ conflicts.ListConflictsFilter) ([]conflicts.ConflictRecordView, error) {
	return f.listRes, f.err
}

func (f fakeConflictSvc) ReviewConflict(_ context.Context, _ identity.AuthContext, cmd conflicts.ReviewConflictCommand) (*conflicts.ConflictRecordView, error) {
	if f.lastCmd != nil {
		*f.lastCmd = cmd
	}
	return f.reviewRes, f.err
}

type fakeConsolidationSvc struct {
	res *consolidation.ConsolidateResult
	err error
}

func (f fakeConsolidationSvc) Consolidate(_ context.Context, _ identity.AuthContext, _ consolidation.ConsolidateCommand) (*consolidation.ConsolidateResult, error) {
	return f.res, f.err
}

type fakeGovernor struct {
	begins atomic.Int32
	ends   atomic.Int32
	snap   governor.Snapshot
}

func (f *fakeGovernor) BeginAgentRequest() func() {
	f.begins.Add(1)
	return func() {
		f.ends.Add(1)
	}
}

func (f *fakeGovernor) Status() governor.Snapshot {
	return f.snap
}

func newPhase03TestRouter(t *testing.T, auth identity.AuthContext, conf rest.ConflictService, cons rest.ConsolidationService, gov rest.GovernorProvider) http.Handler {
	t.Helper()
	h, _, err := rest.NewRouter(rest.RouterDeps{
		Auth:          fakeAuth{auth: auth},
		Capture:       fakeCaptureSvc{},
		Retrieval:     fakeRetrievalSvc{},
		Context:       fakeContextSvc{},
		Feedback:      fakeFeedbackSvc{},
		Conflicts:     conf,
		Consolidation: cons,
		Governor:      gov,
		L1Stats:       fakeL1Stats{},
		Version:       "0.3.0-test",
	})
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}
	return h
}

func TestListConflictsAndResolve(t *testing.T) {
	tenantID := uuid.New()
	projID := uuid.New()
	conflictID := uuid.New()
	fromID := uuid.New()
	toID := uuid.New()
	auth := identity.AuthContext{
		TenantID:      tenantID,
		PrincipalID:   uuid.New(),
		PrincipalType: identity.PrincipalUser,
	}

	view := conflicts.ConflictRecordView{
		ConflictID: conflictID,
		TenantID:   tenantID,
		ProjectID:  &projID,
		FromMemory: memory.Memory{
			ID:              fromID,
			ScopeType:       identity.ScopeProjectShared,
			Status:          memory.StatusActive,
			EpistemicStatus: memory.EpistemicHypothesis,
			Summary:         "old",
			Confidence:      0.5,
		},
		ToMemory: memory.Memory{
			ID:              toID,
			ScopeType:       identity.ScopeProjectShared,
			Status:          memory.StatusActive,
			EpistemicStatus: memory.EpistemicVerified,
			Summary:         "new",
			Confidence:      0.9,
		},
		Weight:   0.85,
		IsActive: true,
	}

	t.Run("GET /api/v1/conflicts returns 200 and invalid project_id returns 422", func(t *testing.T) {
		gov := &fakeGovernor{snap: governor.Snapshot{Profile: governor.ProfileBalanced, State: governor.StateGuard}}
		h := newPhase03TestRouter(t, auth, fakeConflictSvc{listRes: []conflicts.ConflictRecordView{view}}, nil, gov)

		req := httptest.NewRequest(http.MethodGet, "/api/v1/conflicts?project_id="+projID.String(), nil)
		req.Header.Set("Authorization", "Bearer valid-key")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("GET /api/v1/conflicts status = %d, want 200: %s", rec.Code, rec.Body.String())
		}
		if gov.begins.Load() != 1 || gov.ends.Load() != 1 {
			t.Fatalf("expected governor preemption Begin/End (1,1), got (%d,%d)", gov.begins.Load(), gov.ends.Load())
		}

		badReq := httptest.NewRequest(http.MethodGet, "/api/v1/conflicts?project_id=not-a-uuid", nil)
		badReq.Header.Set("Authorization", "Bearer valid-key")
		badRec := httptest.NewRecorder()
		h.ServeHTTP(badRec, badReq)
		if badRec.Code != http.StatusUnprocessableEntity {
			t.Fatalf("bad project_id status = %d, want 422", badRec.Code)
		}
	})

	t.Run("POST /api/v1/conflicts/{id}/resolve maps 200, 403, 404, 422", func(t *testing.T) {
		resolved := view
		resolved.IsActive = false
		h := newPhase03TestRouter(t, auth, fakeConflictSvc{reviewRes: &resolved}, nil, nil)

		body, _ := json.Marshal(map[string]any{"winner_memory_id": toID, "reason": "verified"})
		req := httptest.NewRequest(http.MethodPost, "/api/v1/conflicts/"+conflictID.String()+"/resolve", bytes.NewReader(body))
		req.Header.Set("Authorization", "Bearer valid-key")
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("resolve status = %d, want 200: %s", rec.Code, rec.Body.String())
		}

		// Invalid UUID -> 422
		badUUID := httptest.NewRequest(http.MethodPost, "/api/v1/conflicts/bad/resolve", bytes.NewReader(body))
		badUUID.Header.Set("Authorization", "Bearer valid-key")
		badUUID.Header.Set("Content-Type", "application/json")
		badRec := httptest.NewRecorder()
		h.ServeHTTP(badRec, badUUID)
		if badRec.Code != http.StatusUnprocessableEntity {
			t.Fatalf("bad id status = %d, want 422", badRec.Code)
		}

		// Forbidden -> 403
		hForbid := newPhase03TestRouter(t, auth, fakeConflictSvc{err: conflicts.ErrForbidden}, nil, nil)
		reqF := httptest.NewRequest(http.MethodPost, "/api/v1/conflicts/"+conflictID.String()+"/resolve", bytes.NewReader(body))
		reqF.Header.Set("Authorization", "Bearer valid-key")
		reqF.Header.Set("Content-Type", "application/json")
		recF := httptest.NewRecorder()
		hForbid.ServeHTTP(recF, reqF)
		if recF.Code != http.StatusForbidden {
			t.Fatalf("forbidden status = %d, want 403", recF.Code)
		}

		// Not found -> 404
		hNotFound := newPhase03TestRouter(t, auth, fakeConflictSvc{err: conflicts.ErrNotFound}, nil, nil)
		reqN := httptest.NewRequest(http.MethodPost, "/api/v1/conflicts/"+conflictID.String()+"/resolve", bytes.NewReader(body))
		reqN.Header.Set("Authorization", "Bearer valid-key")
		reqN.Header.Set("Content-Type", "application/json")
		recN := httptest.NewRecorder()
		hNotFound.ServeHTTP(recN, reqN)
		if recN.Code != http.StatusNotFound {
			t.Fatalf("not found status = %d, want 404", recN.Code)
		}
	})
}

func TestAdminConsolidateAndSystemStatusGovernor(t *testing.T) {
	auth := identity.AuthContext{
		TenantID:      uuid.New(),
		PrincipalID:   uuid.New(),
		PrincipalType: identity.PrincipalUser,
		IsAdmin:       true,
	}

	t.Run("POST /api/v1/admin/consolidate returns 200 and maps 403", func(t *testing.T) {
		res := &consolidation.ConsolidateResult{
			ClustersFound:        2,
			DistilledCount:       1,
			ConsolidatedEpisodes: 3,
		}
		gov := &fakeGovernor{snap: governor.Snapshot{Profile: governor.ProfileBalanced, State: governor.StateGuard}}
		h := newPhase03TestRouter(t, auth, nil, fakeConsolidationSvc{res: res}, gov)
		req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/consolidate", bytes.NewReader([]byte(`{"dry_run":true}`)))
		req.Header.Set("Authorization", "Bearer valid-key")
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("consolidate status = %d, want 200: %s", rec.Code, rec.Body.String())
		}
		if gov.begins.Load() != 0 {
			t.Fatalf("expected /api/v1/admin/consolidate not to trigger BeginAgentRequest, got %d", gov.begins.Load())
		}

		hForbid := newPhase03TestRouter(t, auth, nil, fakeConsolidationSvc{err: consolidation.ErrForbidden}, nil)
		reqF := httptest.NewRequest(http.MethodPost, "/api/v1/admin/consolidate", bytes.NewReader([]byte(`{}`)))
		reqF.Header.Set("Authorization", "Bearer valid-key")
		reqF.Header.Set("Content-Type", "application/json")
		recF := httptest.NewRecorder()
		hForbid.ServeHTTP(recF, reqF)
		if recF.Code != http.StatusForbidden {
			t.Fatalf("consolidate forbidden status = %d, want 403", recF.Code)
		}
	})

	t.Run("GET /api/v1/system/status includes Governor snapshot", func(t *testing.T) {
		gov := &fakeGovernor{snap: governor.Snapshot{
			Profile:           governor.ProfileUltra,
			State:             governor.StateBurst,
			PreemptionsCount:  2,
			NLIAvailable:      true,
			RerankerAvailable: true,
			SLMAvailable:      true,
		}}
		h := newPhase03TestRouter(t, auth, nil, nil, gov)
		req := httptest.NewRequest(http.MethodGet, "/api/v1/system/status", nil)
		req.Header.Set("Authorization", "Bearer valid-key")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
		}
		var payload struct {
			ActiveProfile string             `json:"active_profile"`
			Governor      *governor.Snapshot `json:"governor"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
			t.Fatalf("decode status: %v", err)
		}
		if payload.ActiveProfile != string(governor.ProfileUltra) || payload.Governor == nil {
			t.Fatalf("unexpected governor payload: %+v", payload)
		}
		if payload.Governor.PreemptionsCount != 2 || !payload.Governor.NLIAvailable {
			t.Fatalf("unexpected governor fields: %+v", payload.Governor)
		}
		if gov.begins.Load() != 0 {
			t.Fatalf("expected /api/v1/system/status not to trigger BeginAgentRequest, got %d", gov.begins.Load())
		}
	})

	t.Run("POST /api/v1/conflicts/{id}/resolve forwards optional summary and content", func(t *testing.T) {
		var captured conflicts.ReviewConflictCommand
		conflictID := uuid.New()
		winnerID := uuid.New()
		h := newPhase03TestRouter(t, auth, fakeConflictSvc{
			lastCmd:   &captured,
			reviewRes: &conflicts.ConflictRecordView{ConflictID: conflictID},
		}, nil, nil)
		body := []byte(`{"winner_memory_id":"` + winnerID.String() + `","summary":"Merged fact","content":"Merged body","reason":"human synthesis"}`)
		req := httptest.NewRequest(http.MethodPost, "/api/v1/conflicts/"+conflictID.String()+"/resolve", bytes.NewReader(body))
		req.Header.Set("Authorization", "Bearer valid-key")
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("resolve status = %d, want 200: %s", rec.Code, rec.Body.String())
		}
		if captured.Summary != "Merged fact" || captured.Content != "Merged body" || captured.Reason != "human synthesis" {
			t.Fatalf("expected summary/content/reason forwarded, got %+v", captured)
		}
	})

	t.Run("POST /api/v1/admin/consolidate maps ErrEngineUnavailable to 503", func(t *testing.T) {
		h := newPhase03TestRouter(t, auth, nil, fakeConsolidationSvc{err: consolidation.ErrEngineUnavailable}, nil)
		req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/consolidate", bytes.NewReader([]byte(`{}`)))
		req.Header.Set("Authorization", "Bearer valid-key")
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusServiceUnavailable {
			t.Fatalf("consolidate engine unavailable status = %d, want 503: %s", rec.Code, rec.Body.String())
		}
	})
}
