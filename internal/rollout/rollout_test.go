package rollout

import (
	"errors"
	"testing"
)

func TestRollout_Transition_Valid(t *testing.T) {
	r := NewRollout("checkout-svc", "v2.0.0", "v1.9.0")

	if err := r.Transition(StateProgressing); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if r.State != StateProgressing {
		t.Errorf("state = %s, want %s", r.State, StateProgressing)
	}
	if len(r.History) != 1 {
		t.Errorf("history length = %d, want 1", len(r.History))
	}
}

func TestRollout_Transition_Invalid(t *testing.T) {
	r := NewRollout("checkout-svc", "v2.0.0", "v1.9.0")

	err := r.Transition(StatePromoted) // Pending -> Promoted is not allowed
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	var transErr *ErrInvalidTransition
	if !errors.As(err, &transErr) {
		t.Fatalf("expected ErrInvalidTransition, got %T", err)
	}
	if transErr.From != StatePending || transErr.To != StatePromoted {
		t.Errorf("got From=%s To=%s, want From=%s To=%s", transErr.From, transErr.To, StatePending, StatePromoted)
	}
}

func TestRollout_NextTrafficStep(t *testing.T) {
	r := NewRollout("checkout-svc", "v2.0.0", "v1.9.0")

	step, err := r.NextTrafficStep()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if step != 10 {
		t.Errorf("first step = %d, want 10", step)
	}

	r.TrafficPct = 100
	_, err = r.NextTrafficStep()
	if err == nil {
		t.Fatal("expected error when already at 100%, got nil")
	}
}
