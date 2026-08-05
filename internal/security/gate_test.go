package security

import "testing"

func TestGateDecision_ReadOnlyTier(t *testing.T) {
	tests := []struct {
		class    CommandClass
		expected Decision
	}{
		{Read, Allow},
		{Write, Block},
		{Network, Block},
		{Install, Block},
		{Destructive, Block},
	}
	for _, tt := range tests {
		d := GateDecision(tt.class, TierReadOnly)
		if d != tt.expected {
			t.Errorf("readonly + %s: expected %s, got %s", tt.class, tt.expected, d)
		}
	}
}

func TestGateDecision_SupervisedTier(t *testing.T) {
	tests := []struct {
		class    CommandClass
		expected Decision
	}{
		{Read, Allow},
		{Write, Prompt},
		{Network, Prompt},
		{Install, Prompt},
		{Destructive, Prompt},
	}
	for _, tt := range tests {
		d := GateDecision(tt.class, TierSupervised)
		if d != tt.expected {
			t.Errorf("supervised + %s: expected %s, got %s", tt.class, tt.expected, d)
		}
	}
}

func TestGateDecision_FullTier(t *testing.T) {
	tests := []struct {
		class    CommandClass
		expected Decision
	}{
		{Read, Allow},
		{Write, Allow},
		{Network, Prompt},
		{Install, Prompt},
		{Destructive, Prompt},
	}
	for _, tt := range tests {
		d := GateDecision(tt.class, TierFull)
		if d != tt.expected {
			t.Errorf("full + %s: expected %s, got %s", tt.class, tt.expected, d)
		}
	}
}
