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
