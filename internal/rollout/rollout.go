package rollout

import (
	"fmt"
	"time"
)

// Rollout tracks the progressive delivery of one application version
// across a set of target VMs.
type Rollout struct {
	Name         string
	AppVersion   string
	PriorVersion string
	State        State
	TrafficPct   int // current % of traffic on the new version, 0-100
	StartedAt    time.Time
	UpdatedAt    time.Time
	History      []Transition
}

// Transition records one state change for audit/debugging.
type Transition struct {
	From State
	To   State
	At   time.Time
}

// TrafficSteps defines the fixed progression of traffic percentages.
var TrafficSteps = []int{10, 25, 50, 100}

// NewRollout creates a Rollout in the initial Pending state.
func NewRollout(name, appVersion, priorVersion string) *Rollout {
	now := time.Now()
	return &Rollout{
		Name:         name,
		AppVersion:   appVersion,
		PriorVersion: priorVersion,
		State:        StatePending,
		TrafficPct:   0,
		StartedAt:    now,
		UpdatedAt:    now,
	}
}

// Transition attempts to move the rollout to a new state, validating
// the transition against the state machine rules.
func (r *Rollout) Transition(to State) error {
	if !CanTransition(r.State, to) {
		return &ErrInvalidTransition{From: r.State, To: to}
	}
	r.History = append(r.History, Transition{From: r.State, To: to, At: time.Now()})
	r.State = to
	r.UpdatedAt = time.Now()
	return nil
}

// NextTrafficStep returns the next traffic percentage in the progression
// after the current TrafficPct, or an error if already at 100.
func (r *Rollout) NextTrafficStep() (int, error) {
	for _, step := range TrafficSteps {
		if step > r.TrafficPct {
			return step, nil
		}
	}
	return 0, fmt.Errorf("rollout %q already at final traffic step (%d%%)", r.Name, r.TrafficPct)
}
