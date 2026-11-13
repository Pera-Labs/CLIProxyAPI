package management

import "testing"

func TestNormalizeRoutingStrategyWeightedRoundRobin(t *testing.T) {
	for _, input := range []string{"weighted-round-robin", "weightedroundrobin", "wrr"} {
		got, ok := normalizeRoutingStrategy(input)
		if !ok || got != "weighted-round-robin" {
			t.Fatalf("normalizeRoutingStrategy(%q) = %q, %v; want weighted-round-robin, true", input, got, ok)
		}
	}
}

func TestNormalizeRoutingStrategyResetFirst(t *testing.T) {
	for _, input := range []string{"reset-first", "resetfirst"} {
		got, ok := normalizeRoutingStrategy(input)
		if !ok || got != "reset-first" {
			t.Fatalf("normalizeRoutingStrategy(%q) = %q, %v; want reset-first, true", input, got, ok)
		}
	}
}
