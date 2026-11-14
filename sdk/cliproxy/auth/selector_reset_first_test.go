package auth

import (
	"context"
	"encoding/json"
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
	cases := []struct {
		name            string
		fiveHour        string
		weekly          string
		reserve         any
		fiveHourReserve any
		weeklyReserve   any
		wantAvailable   bool
	}{
		{"legacy five-hour floor", "0.70", "0.26", 30, nil, nil, false},
		{"legacy below five-hour floor", "0.69", "0.26", 30, nil, nil, true},
		{"legacy weekly floor", "0.20", "0.70", 30, nil, nil, false},
		{"five-hour override frees current usage", "0.70", "0.26", 30, 10, nil, true},
		{"both windows below their floors", "0.89", "0.69", 30, 10, nil, true},
		{"five-hour override floor", "0.90", "0.26", 30, 10, nil, false},
		{"weekly default remains protected", "0.70", "0.70", 30, 10, nil, false},
		{"weekly override below floor", "0.20", "0.79", 30, nil, 20, true},
		{"weekly override floor", "0.20", "0.80", 30, nil, 20, false},
		{"standalone five-hour override", "0.90", "0.26", nil, 10, nil, false},
		{"no reserve preserves availability", "0.95", "0.95", nil, nil, nil, true},
		{"string default", "0.70", "0.26", "30", nil, nil, false},
		{"json number default", "0.70", "0.26", json.Number("30"), nil, nil, false},
		{"float override", "0.70", "0.26", 30, float64(10), nil, true},
		{"string override", "0.70", "0.26", 30, "10", nil, true},
		{"json number override", "0.70", "0.26", 30, json.Number("10"), nil, true},
		{"invalid override falls back", "0.70", "0.26", 30, "invalid", nil, false},
		{"zero override falls back", "0.70", "0.26", 30, 0, nil, false},
		{"negative override falls back", "0.70", "0.26", 30, -10, nil, false},
		{"hundred override falls back", "0.70", "0.26", 30, 100, nil, false},
		{"nonfinite override falls back", "0.70", "0.26", 30, "NaN", nil, false},
		{"invalid weekly override falls back", "0.20", "0.70", 30, 10, "invalid", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			auth := reservedAuth("a", now.Add(time.Hour), tc.fiveHour)
			auth.Metadata = map[string]any{
				"quota_reserve_percent":    tc.reserve,
				"quota_reserve_5h_percent": tc.fiveHourReserve,
				"quota_reserve_7d_percent": tc.weeklyReserve,
			}
			auth.Quota.Signals["Anthropic-Ratelimit-Unified-7d-Utilization"] = tc.weekly
			if got := IsAuthAvailableForModel(auth, "claude-opus-5-5", now); got != tc.wantAvailable {
				t.Fatalf("available = %v, want %v", got, tc.wantAvailable)
			}
		})
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
