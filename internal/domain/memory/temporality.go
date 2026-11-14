package memory

import "time"

// TriTemporalWindow captures the three independent clocks of a memory or snapshot.
type TriTemporalWindow struct {
	ObservedAt time.Time
	CreatedAt  time.Time
	ValidFrom  time.Time
	ValidUntil *time.Time
}

// PointInTimeQuery requests the memory state known at KnownAt and valid at ValidAt.
type PointInTimeQuery struct {
	AsOf    *time.Time
	ValidAt time.Time
	KnownAt time.Time
}

// Validate checks non-zero clocks and valid_until >= valid_from.
func (w TriTemporalWindow) Validate() error {
	if w.ObservedAt.IsZero() || w.CreatedAt.IsZero() || w.ValidFrom.IsZero() {
		return ErrInvalidTimestamp
	}
	if w.ValidUntil != nil {
		if w.ValidUntil.IsZero() {
			return ErrInvalidTimestamp
		}
		if w.ValidUntil.Before(w.ValidFrom) {
			return ErrInvalidTemporalWindow
		}
	}
	return nil
}

// Covers reports whether validAt falls inside the half-open interval [ValidFrom, ValidUntil).
func (w TriTemporalWindow) Covers(validAt time.Time) bool {
	if validAt.Before(w.ValidFrom) {
		return false
	}
	if w.ValidUntil != nil && !validAt.Before(*w.ValidUntil) {
		return false
	}
	return true
}

// Resolve normalizes AsOf or (ValidAt, KnownAt) into exact non-zero timestamps.
func (q PointInTimeQuery) Resolve() (validAt, knownAt time.Time, err error) {
	if q.AsOf != nil {
		if q.AsOf.IsZero() {
			return time.Time{}, time.Time{}, ErrInvalidPointInTimeQuery
		}
		if !q.ValidAt.IsZero() || !q.KnownAt.IsZero() {
			return time.Time{}, time.Time{}, ErrInvalidPointInTimeQuery
		}
		return *q.AsOf, *q.AsOf, nil
	}
	if q.ValidAt.IsZero() || q.KnownAt.IsZero() {
		return time.Time{}, time.Time{}, ErrInvalidPointInTimeQuery
	}
	return q.ValidAt, q.KnownAt, nil
}

// SelectVersionPointInTime selects the latest immutable version created at or before knownAt
// whose validity interval covers validAt.
func SelectVersionPointInTime(versions []MemoryVersion, query PointInTimeQuery) (*MemoryVersion, error) {
	validAt, knownAt, err := query.Resolve()
	if err != nil {
		return nil, err
	}

	var best *MemoryVersion
	for i := range versions {
		v := &versions[i]
		if !v.CoversPointInTime(validAt, knownAt) {
			continue
		}
		if best == nil || v.CreatedAt.After(best.CreatedAt) || (v.CreatedAt.Equal(best.CreatedAt) && v.Version > best.Version) {
			best = v
		}
	}
	if best == nil {
		return nil, ErrNotFound
	}
	copyBest := *best
	return &copyBest, nil
}
