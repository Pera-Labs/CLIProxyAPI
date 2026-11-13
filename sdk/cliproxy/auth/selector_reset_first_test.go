package auth

import (
	"context"
	"strconv"
	"testing"
	"time"

	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/executor"
)

func resetFirstAuth(id string, weeklyReset time.Time) *Auth {
	auth := &Auth{ID: id}
	if !weeklyReset.IsZero() {
		auth.Quota.Signals = map[string]string{
			"Anthropic-Ratelimit-Unified-7d-Reset": strconv.FormatInt(weeklyReset.Unix(), 10),
		}
	}
	return auth
}

func reservedAuth(id string, weeklyReset time.Time, fiveHourUtilization string) *Auth {
	auth := resetFirstAuth(id, weeklyReset)
	auth.Metadata = map[string]any{"quota_reserve_percent": float64(30)}
	auth.Quota.Signals["Anthropic-Ratelimit-Unified-5h-Utilization"] = fiveHourUtilization
	return auth
}

func TestQuotaReserveBlocksAvailability(t *testing.T) {
	t.Parallel()

	now := time.Now()
	if IsAuthAvailableForModel(reservedAuth("a", now.Add(time.Hour), "0.70"), "claude-sonnet", now) {
		t.Fatal("credential at its reserve floor is available, want blocked")
	}
	if !IsAuthAvailableForModel(reservedAuth("a", now.Add(time.Hour), "0.69"), "claude-sonnet", now) {
		t.Fatal("credential above its reserve floor is blocked, want available")
	}
}

func TestResetFirstSelectorPick(t *testing.T) {
	t.Parallel()

	now := time.Now()
	model := "claude-sonnet"
	tomorrow := now.Add(24 * time.Hour)
	inFourDays := now.Add(96 * time.Hour)

	exhausted := resetFirstAuth("a-exhausted", now.Add(time.Hour))
	exhausted.ModelStates = map[string]*ModelState{
		model: {
			Status:         StatusActive,
			Unavailable:    true,
			NextRetryAfter: now.Add(time.Hour),
			Quota:          QuotaState{Exceeded: true},
		},
	}
	representativeOnly := &Auth{ID: "b-representative", Quota: QuotaState{Signals: map[string]string{
		"anthropic-ratelimit-unified-reset": strconv.FormatInt(tomorrow.Unix(), 10),
	}}}

	cases := []struct {
		name  string
		auths []*Auth
		want  string
	}{
		{"soonest weekly reset wins", []*Auth{resetFirstAuth("a", inFourDays), resetFirstAuth("b", tomorrow)}, "b"},
		{"unobserved credential is probed first", []*Auth{resetFirstAuth("a", tomorrow), resetFirstAuth("b", time.Time{})}, "b"},
		{"stale reset counts as unobserved", []*Auth{resetFirstAuth("a", tomorrow), resetFirstAuth("b", now.Add(-time.Hour))}, "b"},
		{"exhausted credential is skipped", []*Auth{exhausted, resetFirstAuth("b", inFourDays)}, "b"},
		{"representative reset is a fallback", []*Auth{resetFirstAuth("a", inFourDays), representativeOnly}, "b-representative"},
		{"reserve skips a credential near its floor", []*Auth{reservedAuth("a", tomorrow, "0.71"), resetFirstAuth("b", inFourDays)}, "b"},
		{"reserve allows a credential above its floor", []*Auth{reservedAuth("a", tomorrow, "0.69"), resetFirstAuth("b", inFourDays)}, "a"},
		{"ties keep deterministic order", []*Auth{resetFirstAuth("b", tomorrow), resetFirstAuth("a", tomorrow)}, "a"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := (&ResetFirstSelector{}).Pick(context.Background(), "claude", model, cliproxyexecutor.Options{}, tc.auths)
			if err != nil {
				t.Fatalf("Pick() error = %v", err)
			}
			if got.ID != tc.want {
				t.Fatalf("Pick() auth.ID = %q, want %q", got.ID, tc.want)
			}
		})
	}
}
