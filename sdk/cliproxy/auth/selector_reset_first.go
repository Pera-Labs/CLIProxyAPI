package auth

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"time"

	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/executor"
)

// resetFirstSignalKeys lists the observed quota headers that carry a window
// reset time in unix seconds, most authoritative first. The weekly window
// decides which subscription would forfeit quota soonest.
var resetFirstSignalKeys = []string{
	"anthropic-ratelimit-unified-7d-reset",
	"anthropic-ratelimit-unified-reset",
}

// quotaReserveMetadataKey names the auth file field holding the percentage of
// quota the hub must leave unused, for a subscription shared with a person
// who uses it directly.
const quotaReserveMetadataKey = "quota_reserve_percent"

// quotaReserveUtilizationKeys lists the observed utilization headers (0..1)
// checked against a credential's reserve.
var quotaReserveUtilizationKeys = []string{
	"anthropic-ratelimit-unified-5h-utilization",
	"anthropic-ratelimit-unified-7d-utilization",
}

// ResetFirstSelector spends the available credential whose quota window resets
// soonest, because quota left unused at reset is lost. Credentials without a
// current reset observation are picked first so their window gets observed.
// Exhausted credentials are already excluded by cooldown, so the next one is
// used once the soonest-resetting credential runs out.
type ResetFirstSelector struct{}

// Pick selects the available auth with the earliest observed quota reset.
func (s *ResetFirstSelector) Pick(ctx context.Context, provider, model string, opts cliproxyexecutor.Options, auths []*Auth) (*Auth, error) {
	_ = opts
	now := time.Now()
	available, err := getSelectorAvailableAuths(ctx, auths, provider, model, now)
	if err != nil {
		return nil, err
	}
	available = preferCodexWebsocketAuths(ctx, provider, available)
	best := available[0]
	bestReset, bestKnown := observedQuotaResetAt(best, now)
	for _, candidate := range available[1:] {
		reset, known := observedQuotaResetAt(candidate, now)
		if !bestKnown {
			break
		}
		if !known || reset.Before(bestReset) {
			best, bestReset, bestKnown = candidate, reset, known
		}
	}
	return best, nil
}

// observedQuotaResetAt returns the credential's next quota reset. A reset in
// the past is stale: the window rolled over and must be observed again.
func observedQuotaResetAt(auth *Auth, now time.Time) (time.Time, bool) {
	if auth == nil || len(auth.Quota.Signals) == 0 {
		return time.Time{}, false
	}
	for _, key := range resetFirstSignalKeys {
		for name, value := range auth.Quota.Signals {
			if !strings.EqualFold(name, key) {
				continue
			}
			seconds, errParse := strconv.ParseInt(strings.TrimSpace(value), 10, 64)
			if errParse != nil || seconds <= 0 {
				continue
			}
			reset := time.Unix(seconds, 0)
			if !reset.After(now) {
				return time.Time{}, false
			}
			return reset, true
		}
	}
	return time.Time{}, false
}

// quotaReserveReached reports whether the credential's last observed
// utilization in any window leaves no more than its configured reserve.
func quotaReserveReached(auth *Auth) bool {
	reserve, ok := quotaReservePercent(auth)
	if !ok {
		return false
	}
	limit := 1 - reserve/100
	for _, key := range quotaReserveUtilizationKeys {
		for name, value := range auth.Quota.Signals {
			if !strings.EqualFold(name, key) {
				continue
			}
			used, errParse := strconv.ParseFloat(strings.TrimSpace(value), 64)
			if errParse == nil && used >= limit {
				return true
			}
		}
	}
	return false
}

func quotaReservePercent(auth *Auth) (float64, bool) {
	if auth == nil || auth.Metadata == nil {
		return 0, false
	}
	var reserve float64
	switch value := auth.Metadata[quotaReserveMetadataKey].(type) {
	case float64:
		reserve = value
	case int:
		reserve = float64(value)
	case json.Number:
		parsed, errParse := value.Float64()
		if errParse != nil {
			return 0, false
		}
		reserve = parsed
	case string:
		parsed, errParse := strconv.ParseFloat(strings.TrimSpace(value), 64)
		if errParse != nil {
			return 0, false
		}
		reserve = parsed
	default:
		return 0, false
	}
	if reserve <= 0 || reserve >= 100 {
		return 0, false
	}
	return reserve, true
}
