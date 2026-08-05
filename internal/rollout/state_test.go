package rollout

import "testing"

func TestCanTransition(t *testing.T) {
	tests := []struct {
		name string
		from State
		to   State
		want bool
	}{
		{"pending to progressing is valid", StatePending, StateProgressing, true},
		{"progressing to analyzing is valid", StateProgressing, StateAnalyzing, true},
		{"analyzing to progressing loop is valid", StateAnalyzing, StateProgressing, true},
		{"analyzing to promoted is valid", StateAnalyzing, StatePromoted, true},
		{"analyzing to rolledback is valid", StateAnalyzing, StateRolledBack, true},
		{"pending to promoted is invalid", StatePending, StatePromoted, false},
		{"promoted has no outgoing edges", StatePromoted, StateProgressing, false},
		{"rolledback has no outgoing edges", StateRolledBack, StateProgressing, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := CanTransition(tt.from, tt.to)
			if got != tt.want {
				t.Errorf("CanTransition(%s, %s) = %v, want %v", tt.from, tt.to, got, tt.want)
			}
		})
	}
}

func TestIsTerminal(t *testing.T) {
	if !IsTerminal(StatePromoted) {
		t.Error("expected Promoted to be terminal")
	}
	if !IsTerminal(StateRolledBack) {
		t.Error("expected RolledBack to be terminal")
	}
	if IsTerminal(StatePending) {
		t.Error("expected Pending to not be terminal")
	}
}
