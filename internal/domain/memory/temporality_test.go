package memory_test

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/romero-project/romero/internal/domain/memory"
)

func TestTriTemporalWindow(t *testing.T) {
	t.Parallel()

	t0 := time.Date(2026, 11, 14, 10, 0, 0, 0, time.UTC)
	tBefore := t0.Add(-time.Minute)
	t1 := t0.Add(time.Hour)
	t2 := t1.Add(time.Hour)
	zeroTime := time.Time{}

	tests := []struct {
		name    string
		window  memory.TriTemporalWindow
		probe   time.Time
		wantErr error
		covers  bool
	}{
		{
			name:   "open ended valid window covers exact valid_from",
			window: memory.TriTemporalWindow{ObservedAt: t0, CreatedAt: t0, ValidFrom: t0},
			probe:  t0,
			covers: true,
		},
		{
			name:   "open ended valid window excludes before valid_from",
			window: memory.TriTemporalWindow{ObservedAt: t0, CreatedAt: t0, ValidFrom: t0},
			probe:  tBefore,
			covers: false,
		},
		{
			name:   "open ended valid window covers after valid_from",
			window: memory.TriTemporalWindow{ObservedAt: t0, CreatedAt: t0, ValidFrom: t0},
			probe:  t1,
			covers: true,
		},
		{
			name:   "closed half-open [t0, t2) covers t0 and excludes t2",
			window: memory.TriTemporalWindow{ObservedAt: t0, CreatedAt: t0, ValidFrom: t0, ValidUntil: &t2},
			probe:  t0,
			covers: true,
		},
		{
			name:   "closed half-open [t0, t2) excludes exact valid_until",
			window: memory.TriTemporalWindow{ObservedAt: t0, CreatedAt: t0, ValidFrom: t0, ValidUntil: &t2},
			probe:  t2,
			covers: false,
		},
		{
			name:   "valid_until equal to valid_from is valid per SQL >= check and covers nothing",
			window: memory.TriTemporalWindow{ObservedAt: t0, CreatedAt: t0, ValidFrom: t1, ValidUntil: &t1},
			probe:  t1,
			covers: false,
		},
		{
			name:    "valid_until before valid_from rejected",
			window:  memory.TriTemporalWindow{ObservedAt: t0, CreatedAt: t0, ValidFrom: t1, ValidUntil: &t0},
			wantErr: memory.ErrInvalidTemporalWindow,
		},
		{
			name:    "non-nil zero valid_until rejected",
			window:  memory.TriTemporalWindow{ObservedAt: t0, CreatedAt: t0, ValidFrom: t0, ValidUntil: &zeroTime},
			wantErr: memory.ErrInvalidTimestamp,
		},
		{
			name:    "missing observed_at rejected",
			window:  memory.TriTemporalWindow{CreatedAt: t0, ValidFrom: t0},
			wantErr: memory.ErrInvalidTimestamp,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := tc.window.Validate()
			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("Validate() error = %v, want %v", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("Validate() unexpected error = %v", err)
			}
			if got := tc.window.Covers(tc.probe); got != tc.covers {
				t.Errorf("Covers(%v) = %v, want %v", tc.probe, got, tc.covers)
			}
		})
	}
}

func TestSelectVersionPointInTime(t *testing.T) {
	t.Parallel()

	memID := uuid.New()
	t0 := time.Date(2026, 11, 14, 9, 0, 0, 0, time.UTC)
	t1 := t0.Add(time.Hour)
	t2 := t1.Add(time.Hour)
	t3 := t2.Add(time.Hour)
	zeroTime := time.Time{}

	versions := []memory.MemoryVersion{
		{
			ID:         uuid.New(),
			MemoryID:   memID,
			Version:    1,
			Summary:    "v1 summary",
			CreatedAt:  t1,
			ValidFrom:  t1,
			ValidUntil: &t3,
			Status:     memory.StatusActive,
		},
		{
			ID:        uuid.New(),
			MemoryID:  memID,
			Version:   2,
			Summary:   "v2 summary",
			CreatedAt: t3,
			ValidFrom: t3,
			Status:    memory.StatusActive,
		},
		{
			ID:        uuid.New(),
			MemoryID:  memID,
			Version:   3,
			Summary:   "v3 same createdAt higher version",
			CreatedAt: t3,
			ValidFrom: t3,
			Status:    memory.StatusActive,
		},
	}

	tests := []struct {
		name        string
		query       memory.PointInTimeQuery
		wantVersion int
		wantErr     error
	}{
		{
			name:        "AC-07 at t2 returns v1 without v2 leak",
			query:       memory.PointInTimeQuery{ValidAt: t2, KnownAt: t2},
			wantVersion: 1,
		},
		{
			name:    "AC-07 before creation returns ErrNotFound",
			query:   memory.PointInTimeQuery{ValidAt: t0, KnownAt: t0},
			wantErr: memory.ErrNotFound,
		},
		{
			name:        "as_of at t3 tie-breaks on highest version",
			query:       memory.PointInTimeQuery{AsOf: &t3},
			wantVersion: 3,
		},
		{
			name:    "empty query rejected",
			query:   memory.PointInTimeQuery{},
			wantErr: memory.ErrInvalidPointInTimeQuery,
		},
		{
			name:    "zero as_of rejected",
			query:   memory.PointInTimeQuery{AsOf: &zeroTime},
			wantErr: memory.ErrInvalidPointInTimeQuery,
		},
		{
			name:    "mixed as_of and valid_at rejected",
			query:   memory.PointInTimeQuery{AsOf: &t2, ValidAt: t2},
			wantErr: memory.ErrInvalidPointInTimeQuery,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := memory.SelectVersionPointInTime(versions, tc.query)
			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("SelectVersionPointInTime() error = %v, want %v", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("SelectVersionPointInTime() unexpected error = %v", err)
			}
			if got == nil || got.Version != tc.wantVersion {
				t.Errorf("selected version = %v, want %d", got, tc.wantVersion)
			}
		})
	}
}
