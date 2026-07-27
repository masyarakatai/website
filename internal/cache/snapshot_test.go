package cache

import (
	"errors"
	"testing"
	"time"
)

func TestStore_StartsUnavailable(t *testing.T) {
	s := NewStore[[]string]()
	d := s.Load()

	if d.State != Unavailable {
		t.Errorf("State = %v, want Unavailable", d.State)
	}
	if d.OK() {
		t.Error("OK() = true on a store that has never fetched, want false")
	}
}

func TestStore_PublishMakesFresh(t *testing.T) {
	s := NewStore[[]string]()
	now := time.Date(2026, 7, 27, 12, 0, 0, 0, time.UTC)
	s.Publish([]string{"a", "b"}, now)

	d := s.Load()
	if d.State != Fresh {
		t.Errorf("State = %v, want Fresh", d.State)
	}
	if len(d.Value) != 2 {
		t.Errorf("Value length = %d, want 2", len(d.Value))
	}
	if !d.LastSuccess.Equal(now) {
		t.Errorf("LastSuccess = %v, want %v", d.LastSuccess, now)
	}
}

// The core guarantee of this package: a failed refresh must not discard data or
// present a zero value as real. Losing this means a transient GitHub blip blanks
// the site.
func TestStore_FailPreservesPreviousValue(t *testing.T) {
	s := NewStore[[]string]()
	now := time.Date(2026, 7, 27, 12, 0, 0, 0, time.UTC)
	s.Publish([]string{"a", "b", "c"}, now)

	s.Fail(errors.New("github: 500 internal server error"))

	d := s.Load()
	if d.State != Stale {
		t.Errorf("State = %v, want Stale after a failure following a success", d.State)
	}
	if len(d.Value) != 3 {
		t.Fatalf("Value length = %d, want the previous 3 entries preserved", len(d.Value))
	}
	if !d.LastSuccess.Equal(now) {
		t.Errorf("LastSuccess = %v, want the original success time %v", d.LastSuccess, now)
	}
	if !d.OK() {
		t.Error("OK() = false for stale data, want true -- stale data is still worth rendering")
	}
	if d.LastError == "" {
		t.Error("LastError is empty, want the failure recorded")
	}
}

// A failure before any success must stay Unavailable. Promoting it to Stale
// would make an empty zero value look like real data that merely went stale --
// exactly the conflation this design exists to prevent.
func TestStore_FailBeforeAnySuccessStaysUnavailable(t *testing.T) {
	s := NewStore[[]string]()
	s.Fail(errors.New("github: connection refused"))

	d := s.Load()
	if d.State != Unavailable {
		t.Errorf("State = %v, want Unavailable when nothing has ever succeeded", d.State)
	}
	if d.OK() {
		t.Error("OK() = true, want false -- there is no real data to render")
	}
}

// A 304 is a success: the data is confirmed current. Treating it as a failure
// would flap the whole site into "stale" every poll where nothing changed,
// which is the common case.
func TestStore_KeepFreshOnNotModified(t *testing.T) {
	s := NewStore[[]string]()
	t0 := time.Date(2026, 7, 27, 12, 0, 0, 0, time.UTC)
	t1 := t0.Add(5 * time.Minute)

	s.Publish([]string{"x"}, t0)
	s.KeepFresh(t1)

	d := s.Load()
	if d.State != Fresh {
		t.Errorf("State = %v, want Fresh after a 304", d.State)
	}
	if len(d.Value) != 1 || d.Value[0] != "x" {
		t.Errorf("Value = %v, want the previous value preserved", d.Value)
	}
	if !d.LastSuccess.Equal(t1) {
		t.Errorf("LastSuccess = %v, want it advanced to %v", d.LastSuccess, t1)
	}
}

func TestStore_KeepFreshDoesNothingWhenUnavailable(t *testing.T) {
	s := NewStore[[]string]()
	s.KeepFresh(time.Now())

	if got := s.Load().State; got != Unavailable {
		t.Errorf("State = %v, want Unavailable -- a 304 with no prior data must not fabricate freshness", got)
	}
}

func TestStore_RecoversAfterFailure(t *testing.T) {
	s := NewStore[[]string]()
	t0 := time.Date(2026, 7, 27, 12, 0, 0, 0, time.UTC)
	s.Publish([]string{"old"}, t0)
	s.Fail(errors.New("boom"))
	s.Publish([]string{"new"}, t0.Add(time.Minute))

	d := s.Load()
	if d.State != Fresh {
		t.Errorf("State = %v, want Fresh after recovery", d.State)
	}
	if d.Value[0] != "new" {
		t.Errorf("Value = %v, want the new value", d.Value)
	}
	if d.LastError != "" {
		t.Errorf("LastError = %q, want cleared after a successful refresh", d.LastError)
	}
}

func TestFreshness_String(t *testing.T) {
	for _, tc := range []struct {
		f    Freshness
		want string
	}{
		{Fresh, "fresh"},
		{Stale, "stale"},
		{Unavailable, "unavailable"},
	} {
		if got := tc.f.String(); got != tc.want {
			t.Errorf("Freshness(%d).String() = %q, want %q", tc.f, got, tc.want)
		}
	}
}
