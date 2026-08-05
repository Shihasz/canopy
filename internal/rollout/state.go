package rollout

import "fmt"

// State represents where a Rollout currently is in its lifecycle.
type State string

const (
	StatePending     State = "Pending"
	StateProgressing State = "Progressing"
	StateAnalyzing   State = "Analyzing"
	StatePromoted    State = "Promoted"
	StateRolledBack  State = "RolledBack"
)

// validTransitions defines the allowed State -> []State edges.
// Promoted and RolledBack are terminal: no outgoing edges.
var validTransitions = map[State][]State{
	StatePending:     {StateProgressing},
	StateProgressing: {StateAnalyzing},
	StateAnalyzing:   {StateProgressing, StatePromoted, StateRolledBack},
	StatePromoted:    {},
	StateRolledBack:  {},
}

// ErrInvalidTransition is returned when a transition is not allowed.
type ErrInvalidTransition struct {
	From State
	To   State
}

func (e *ErrInvalidTransition) Error() string {
	return fmt.Sprintf("invalid rollout state transition: %s -> %s", e.From, e.To)
}

// CanTransition reports whether moving from `from` to `to` is allowed.
func CanTransition(from, to State) bool {
	for _, allowed := range validTransitions[from] {
		if allowed == to {
			return true
		}
	}
	return false
}

// IsTerminal reports whether a state has no valid outgoing transitions.
func IsTerminal(s State) bool {
	return len(validTransitions[s]) == 0
}
