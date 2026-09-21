// Package modelquota defines shared provider admission arithmetic. Database
// owners serialize one quota group and supply their authoritative clock.
package modelquota

import (
	"errors"
	"math"
	"regexp"
	"sort"
	"strings"

	"github.com/xyzbit/agentkiln/execution/backend/contract"
)

const (
	// MinuteMillis is the rate accounting window, not a configurable duration.
	MinuteMillis int64 = 60_000
	// MaxTokens bounds arithmetic and request estimates at the wire boundary.
	MaxTokens int64 = 1 << 40
)

var (
	ErrInvalid  = errors.New("invalid model quota configuration or request")
	ErrConflict = errors.New("model quota reservation conflicts with the original request")
	ErrInactive = errors.New("model quota requires the current running execution attempt")
	ErrTooLarge = errors.New("model request exceeds the shared token allowance")
)

// Validate checks an operator-owned quota definition before persistence.
func Validate(value contract.ModelQuota) error {
	if !regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`).MatchString(value.ID) || len(value.Connections) == 0 || len(value.Connections) > 256 || value.RequestsPerMinute < 0 || value.RequestsPerMinute > MaxTokens || value.TokensPerMinute < 0 || value.TokensPerMinute > MaxTokens || value.RequestsPerMinute+value.TokensPerMinute == 0 || value.MaxOutputTokens < 1 || value.MaxOutputTokens > MaxTokens || (value.TokensPerMinute > 0 && value.MaxOutputTokens >= value.TokensPerMinute) {
		return ErrInvalid
	}
	seen := map[string]bool{}
	for _, id := range value.Connections {
		if id == "" || id != strings.TrimSpace(id) || len(id) > 256 || seen[id] {
			return ErrInvalid
		}
		seen[id] = true
	}
	return nil
}

// Reservation retains an expired grant's debit through its possible start
// interval plus a minute. Reconciliation changes tokens but never the expiry.
type Reservation struct {
	Sequence     int
	InputTokens  int64
	Tokens       int64
	ValidUntilMs int64
	ExpiresAtMs  int64
}

// ValidateAttempt rejects stale owners, terminal work and out-of-budget calls.
func ValidateAttempt(value contract.Execution, attempt int, input contract.ModelPermitRequest, plan *contract.ModelQuotaPlan) error {
	if value.Status != contract.StatusRunning || value.Attempt != attempt || value.StartedAt == nil || plan == nil {
		return ErrInactive
	}
	if input.Sequence < 1 || input.Sequence > value.MaxModelRequests || input.InputTokens < 0 || input.InputTokens > MaxTokens || plan.MaxOutputTokens < 1 || plan.MaxOutputTokens > MaxTokens {
		return ErrInvalid
	}
	return nil
}

// Decide returns a conservative wait from all live debits in one group.
// Unknown usage keeps the full estimate. The input slice may be reordered.
func Decide(quota contract.ModelQuota, records []Reservation, tokens, now int64) (contract.ModelPermit, error) {
	if tokens < 1 || tokens > MaxTokens || (quota.TokensPerMinute > 0 && tokens > quota.TokensPerMinute) {
		return contract.ModelPermit{}, ErrTooLarge
	}
	var requests, used, maxExpiry int64
	overflow := false
	for _, record := range records {
		if record.ExpiresAtMs > now {
			requests++
			maxExpiry = max(maxExpiry, record.ExpiresAtMs)
			if record.Tokens > math.MaxInt64-used {
				overflow = true
			} else {
				used += record.Tokens
			}
		}
	}
	if overflow {
		return contract.ModelPermit{RetryAfterMs: maxExpiry - now}, nil
	}
	fits := func() bool {
		return (quota.RequestsPerMinute == 0 || requests < quota.RequestsPerMinute) && (quota.TokensPerMinute == 0 || tokens <= quota.TokensPerMinute-used)
	}
	if fits() {
		return contract.ModelPermit{Granted: true}, nil
	}
	sort.Slice(records, func(i, j int) bool { return records[i].ExpiresAtMs < records[j].ExpiresAtMs })
	for _, record := range records {
		if record.ExpiresAtMs <= now {
			continue
		}
		requests--
		used -= record.Tokens
		if fits() {
			return contract.ModelPermit{RetryAfterMs: record.ExpiresAtMs - now}, nil
		}
	}
	return contract.ModelPermit{RetryAfterMs: MinuteMillis}, nil
}
