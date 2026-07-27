// Package cache holds GitHub data in memory and refreshes it in the background.
//
// The central rule: HTTP handlers never call GitHub. A poller publishes
// immutable snapshots and handlers do a single atomic load. Three properties
// follow, and the whole design exists for them:
//
//  1. Page loads never block on GitHub. Latency is template rendering only.
//  2. Visitor traffic cannot consume rate limit. Polling cost is fixed.
//  3. Degradation is graceful by default: a failed refresh leaves the previous
//     snapshot serving, marked stale.
package cache

import (
	"sync/atomic"
	"time"
)

// Freshness distinguishes the three states a dataset can be in. This exists so
// that a failed fetch and a genuinely empty result can never render the same
// way. An empty leaderboard because nobody has contributed is an invitation; an
// empty leaderboard because the API returned 500 is a bug being hidden.
type Freshness int

const (
	// Unavailable means no successful fetch has ever completed. There is no
	// data to show, and the page must say so rather than showing zero.
	Unavailable Freshness = iota
	// Stale means data was fetched successfully at some point but the most
	// recent refresh failed. Serve what we have, and disclose its age.
	Stale
	// Fresh means the most recent refresh succeeded.
	Fresh
)

func (f Freshness) String() string {
	switch f {
	case Fresh:
		return "fresh"
	case Stale:
		return "stale"
	default:
		return "unavailable"
	}
}

// Dataset wraps a value with its freshness and the time it was last confirmed
// good. The zero Dataset is correctly Unavailable.
type Dataset[T any] struct {
	Value       T
	State       Freshness
	LastSuccess time.Time
	// LastError is the most recent refresh failure, retained so the operator
	// can see the cause in a debug view without digging through logs.
	LastError string
}

// OK reports whether there is data worth rendering. Stale data is worth
// rendering; unavailable data is not.
func (d Dataset[T]) OK() bool { return d.State != Unavailable }

// Store holds one Dataset per field behind an atomic pointer. Readers get a
// consistent view without locking; the poller swaps whole snapshots.
type Store[T any] struct {
	ptr atomic.Pointer[Dataset[T]]
}

// NewStore returns a Store in the Unavailable state.
func NewStore[T any]() *Store[T] {
	s := &Store[T]{}
	s.ptr.Store(&Dataset[T]{State: Unavailable})
	return s
}

// Load returns the current dataset. Never blocks, never returns nil.
func (s *Store[T]) Load() Dataset[T] {
	d := s.ptr.Load()
	if d == nil {
		return Dataset[T]{State: Unavailable}
	}
	return *d
}

// Publish records a successful refresh, replacing the value and marking it
// Fresh.
func (s *Store[T]) Publish(v T, now time.Time) {
	s.ptr.Store(&Dataset[T]{Value: v, State: Fresh, LastSuccess: now})
}

// Fail records a failed refresh.
//
// The previously-published value is preserved and demoted to Stale. If nothing
// has ever succeeded the dataset stays Unavailable -- it must not silently
// present a zero value as though it were real data.
func (s *Store[T]) Fail(err error) {
	prev := s.Load()
	next := Dataset[T]{
		Value:       prev.Value,
		LastSuccess: prev.LastSuccess,
		LastError:   err.Error(),
	}
	if prev.State == Unavailable {
		next.State = Unavailable
	} else {
		next.State = Stale
	}
	s.ptr.Store(&next)
}

// KeepFresh re-affirms the existing value without changing it, used when GitHub
// answers 304 Not Modified. The data is confirmed current, so it is Fresh --
// treating a 304 as a failure would flap the whole site into "stale" every time
// nothing changed, which is most of the time.
func (s *Store[T]) KeepFresh(now time.Time) {
	prev := s.Load()
	if prev.State == Unavailable {
		// A 304 before any successful fetch would mean we cached an ETag
		// without data, which should not happen; stay honest about it.
		return
	}
	s.ptr.Store(&Dataset[T]{
		Value:       prev.Value,
		State:       Fresh,
		LastSuccess: now,
	})
}
