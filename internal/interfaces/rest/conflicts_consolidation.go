package rest

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/romero-project/romero/internal/application/conflicts"
	"github.com/romero-project/romero/internal/application/consolidation"
	"github.com/romero-project/romero/internal/domain/governor"
	"github.com/romero-project/romero/internal/domain/identity"
	"github.com/romero-project/romero/internal/domain/memory"
)

// ConflictService defines conflict listing and arbitration operations exposed by REST.
type ConflictService interface {
	ListConflicts(ctx context.Context, auth identity.AuthContext, filter conflicts.ListConflictsFilter) ([]conflicts.ConflictRecordView, error)
	ReviewConflict(ctx context.Context, auth identity.AuthContext, cmd conflicts.ReviewConflictCommand) (*conflicts.ConflictRecordView, error)
}

// ConsolidationService defines REM consolidation operations exposed by REST.
type ConsolidationService interface {
	Consolidate(ctx context.Context, auth identity.AuthContext, cmd consolidation.ConsolidateCommand) (*consolidation.ConsolidateResult, error)
}

// GovernorProvider exposes PowerGovernor preemption hooks and status snapshots to REST.
type GovernorProvider interface {
	BeginAgentRequest() func()
	Status() governor.Snapshot
}

type listConflictsInput struct {
	ProjectID string `query:"project_id"`
	Limit     int    `query:"limit"`
	Offset    int    `query:"offset"`
}

type conflictMemoryDTO struct {
	ID              uuid.UUID              `json:"id"`
	ScopeType       identity.ScopeType     `json:"scope_type"`
	Status          memory.MemoryStatus    `json:"status"`
	EpistemicStatus memory.EpistemicStatus `json:"epistemic_status"`
	TopicKey        string                 `json:"topic_key,omitempty"`
	Summary         string                 `json:"summary"`
	Confidence      float32                `json:"confidence"`
}

type conflictRecordDTO struct {
	ConflictID       uuid.UUID         `json:"conflict_id"`
	TenantID         uuid.UUID         `json:"tenant_id"`
	ProjectID        *uuid.UUID        `json:"project_id,omitempty"`
	FromMemory       conflictMemoryDTO `json:"from_memory"`
	ToMemory         conflictMemoryDTO `json:"to_memory"`
	Weight           float32           `json:"weight"`
	NLIEntailment    *float32          `json:"nli_entailment,omitempty"`
	NLIContradiction *float32          `json:"nli_contradiction,omitempty"`
	IsActive         bool              `json:"is_active"`
	Notes            string            `json:"notes,omitempty"`
}

type listConflictsOutput struct {
	Status int
	Body   struct {
		Conflicts []conflictRecordDTO `json:"conflicts"`
	}
}

type resolveConflictInput struct {
	ID   string `path:"id"`
	Body struct {
		WinnerMemoryID uuid.UUID `json:"winner_memory_id"`
		Summary        string    `json:"summary,omitempty"`
		Content        string    `json:"content,omitempty"`
		Reason         string    `json:"reason,omitempty"`
	}
}

type resolveConflictOutput struct {
	Status int
	Body   conflictRecordDTO
}

type consolidateInput struct {
	Body struct {
		ProjectID *uuid.UUID `json:"project_id,omitempty"`
		DryRun    bool       `json:"dry_run,omitempty"`
	}
}

type consolidateOutput struct {
	Status int
	Body   struct {
		ClustersFound        int                           `json:"clusters_found"`
		DistilledCount       int                           `json:"distilled_count"`
		ConsolidatedEpisodes int                           `json:"consolidated_episodes"`
		Preempted            bool                          `json:"preempted"`
		CreatedMemoryIDs     []uuid.UUID                   `json:"created_memory_ids,omitempty"`
		Drafts               []memory.DistilledMemoryDraft `json:"drafts,omitempty"`
	}
}

func governorPreemptionMiddleware(gov GovernorProvider, next http.Handler) http.Handler {
	if gov == nil {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		if strings.HasPrefix(path, "/api/v1/") &&
			path != "/api/v1/admin/consolidate" &&
			path != "/api/v1/system/status" {
			end := gov.BeginAgentRequest()
			defer end()
		}
		next.ServeHTTP(w, r)
	})
}

func registerPhase03Routes(api huma.API, deps RouterDeps) {
	if deps.Conflicts != nil {
		huma.Register(api, huma.Operation{
			OperationID: "list-conflicts",
			Method:      http.MethodGet,
			Path:        "/api/v1/conflicts",
			Summary:     "List active NLI contradiction pairs visible to the caller",
		}, func(ctx context.Context, in *listConflictsInput) (*listConflictsOutput, error) {
			auth, err := callerFromCtx(ctx)
			if err != nil {
				return nil, err
			}
			var projID *uuid.UUID
			if strings.TrimSpace(in.ProjectID) != "" {
				parsed, pErr := uuid.Parse(strings.TrimSpace(in.ProjectID))
				if pErr != nil {
					return nil, huma.Error422UnprocessableEntity("invalid project_id")
				}
				projID = &parsed
			}
			views, err := deps.Conflicts.ListConflicts(ctx, auth, conflicts.ListConflictsFilter{
				ProjectID: projID,
				Limit:     in.Limit,
				Offset:    in.Offset,
			})
			if err != nil {
				return nil, mapConflictServiceErr(err)
			}
			out := &listConflictsOutput{Status: http.StatusOK}
			out.Body.Conflicts = make([]conflictRecordDTO, 0, len(views))
			for _, v := range views {
				out.Body.Conflicts = append(out.Body.Conflicts, toConflictDTO(v))
			}
			return out, nil
		})

		huma.Register(api, huma.Operation{
			OperationID: "resolve-conflict",
			Method:      http.MethodPost,
			Path:        "/api/v1/conflicts/{id}/resolve",
			Summary:     "Atomically arbitrate an active contradiction pair",
		}, func(ctx context.Context, in *resolveConflictInput) (*resolveConflictOutput, error) {
			auth, err := callerFromCtx(ctx)
			if err != nil {
				return nil, err
			}
			conflictID, err := uuid.Parse(strings.TrimSpace(in.ID))
			if err != nil {
				return nil, huma.Error422UnprocessableEntity("invalid conflict id")
			}
			view, err := deps.Conflicts.ReviewConflict(ctx, auth, conflicts.ReviewConflictCommand{
				ConflictID:     conflictID,
				WinnerMemoryID: in.Body.WinnerMemoryID,
				Summary:        in.Body.Summary,
				Content:        in.Body.Content,
				Reason:         in.Body.Reason,
				CorrelationID:  "",
			})
			if err != nil {
				return nil, mapConflictServiceErr(err)
			}
			return &resolveConflictOutput{
				Status: http.StatusOK,
				Body:   toConflictDTO(*view),
			}, nil
		})
	}

	if deps.Consolidation != nil {
		huma.Register(api, huma.Operation{
			OperationID: "trigger-rem-consolidation",
			Method:      http.MethodPost,
			Path:        "/api/v1/admin/consolidate",
			Summary:     "Trigger preemptible REM sleep episodic consolidation",
		}, func(ctx context.Context, in *consolidateInput) (*consolidateOutput, error) {
			auth, err := callerFromCtx(ctx)
			if err != nil {
				return nil, err
			}
			res, err := deps.Consolidation.Consolidate(ctx, auth, consolidation.ConsolidateCommand{
				ProjectID:     in.Body.ProjectID,
				DryRun:        in.Body.DryRun,
				CorrelationID: "",
			})
			if err != nil {
				return nil, mapConsolidationServiceErr(err)
			}
			out := &consolidateOutput{Status: http.StatusOK}
			out.Body.ClustersFound = res.ClustersFound
			out.Body.DistilledCount = res.DistilledCount
			out.Body.ConsolidatedEpisodes = res.ConsolidatedEpisodes
			out.Body.Preempted = res.Preempted
			out.Body.CreatedMemoryIDs = res.CreatedMemoryIDs
			out.Body.Drafts = res.Drafts
			return out, nil
		})
	}
}

func mapConflictServiceErr(err error) error {
	switch {
	case errors.Is(err, conflicts.ErrUnauthorized):
		return huma.Error401Unauthorized("unauthorized")
	case errors.Is(err, conflicts.ErrForbidden):
		return huma.Error403Forbidden("forbidden")
	case errors.Is(err, conflicts.ErrNotFound):
		return huma.Error404NotFound("conflict not found")
	case errors.Is(err, conflicts.ErrInvalidWinner), errors.Is(err, conflicts.ErrValidation):
		return huma.Error422UnprocessableEntity(err.Error())
	default:
		return huma.Error503ServiceUnavailable("conflict service unavailable")
	}
}

func mapConsolidationServiceErr(err error) error {
	switch {
	case errors.Is(err, consolidation.ErrUnauthorized):
		return huma.Error401Unauthorized("unauthorized")
	case errors.Is(err, consolidation.ErrForbidden):
		return huma.Error403Forbidden("forbidden")
	case errors.Is(err, consolidation.ErrEngineUnavailable):
		return huma.Error503ServiceUnavailable("consolidation engine unavailable")
	default:
		return huma.Error503ServiceUnavailable("consolidation service unavailable")
	}
}

func toConflictDTO(v conflicts.ConflictRecordView) conflictRecordDTO {
	return conflictRecordDTO{
		ConflictID: v.ConflictID,
		TenantID:   v.TenantID,
		ProjectID:  v.ProjectID,
		FromMemory: conflictMemoryDTO{
			ID:              v.FromMemory.ID,
			ScopeType:       v.FromMemory.ScopeType,
			Status:          v.FromMemory.Status,
			EpistemicStatus: v.FromMemory.EpistemicStatus,
			TopicKey:        v.FromMemory.TopicKey,
			Summary:         v.FromMemory.Summary,
			Confidence:      v.FromMemory.Confidence,
		},
		ToMemory: conflictMemoryDTO{
			ID:              v.ToMemory.ID,
			ScopeType:       v.ToMemory.ScopeType,
			Status:          v.ToMemory.Status,
			EpistemicStatus: v.ToMemory.EpistemicStatus,
			TopicKey:        v.ToMemory.TopicKey,
			Summary:         v.ToMemory.Summary,
			Confidence:      v.ToMemory.Confidence,
		},
		Weight:           v.Weight,
		NLIEntailment:    v.NLIEntailment,
		NLIContradiction: v.NLIContradiction,
		IsActive:         v.IsActive,
		Notes:            v.Notes,
	}
}
