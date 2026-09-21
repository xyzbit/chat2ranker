package modelquota

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/xyzbit/agentkiln/execution/backend/contract"
)

// Repository owns durable group configuration and per-request debits.
type Repository interface {
	ConfigureModelQuota(context.Context, contract.ModelQuota) error
	ModelQuotaForConnection(context.Context, string) (*contract.ModelQuotaPlan, error)
	ReserveModelPermit(context.Context, string, int, contract.ModelPermitRequest, time.Duration) (contract.ModelPermit, error)
	ReconcileModelPermit(context.Context, string, int, int, int64) error
}

// Authority issues invocation-only credentials. Its key never enters a worker
// request, runtime patch, model context or artifact.
type Authority struct {
	key            []byte
	endpoint       string
	validity       time.Duration
	requestTimeout time.Duration
}

type claims struct {
	ID        string `json:"id"`
	Attempt   int    `json:"attempt"`
	ExpiresAt int64  `json:"expiresAt"`
}

// NewAuthority accepts an explicit runtime-reachable endpoint and a dedicated
// high-entropy key. Empty configuration disables this optional capability.
func NewAuthority(endpoint, key string, validity, requestTimeout time.Duration) (*Authority, error) {
	if endpoint == "" && key == "" {
		return nil, nil
	}
	u, err := url.Parse(endpoint)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Path != "/v1/model-permits" || len(key) < 32 || validity < time.Millisecond || validity > time.Second || requestTimeout < time.Millisecond || requestTimeout > time.Minute {
		return nil, errors.New("model quota needs an HTTP(S) /v1/model-permits URL, a dedicated key of at least 32 characters and permit validity within 1ms..1s and callback timeout within 1ms..1m")
	}
	return &Authority{key: []byte(key), endpoint: endpoint, validity: validity, requestTimeout: requestTimeout}, nil
}

// Issue binds callback access to one running attempt and its fixed deadline.
func (a *Authority) Issue(value contract.Execution) (*contract.ModelQuotaRuntime, error) {
	if value.ModelQuota == nil {
		return nil, nil
	}
	if a == nil || value.StartedAt == nil || value.Attempt < 1 || value.RuntimeTimeoutMs <= 0 {
		return nil, ErrInactive
	}
	deadline := value.StartedAt.Add(time.Duration(value.RuntimeTimeoutMs) * time.Millisecond)
	body, _ := json.Marshal(claims{ID: value.ID, Attempt: value.Attempt, ExpiresAt: deadline.UnixMilli()})
	payload := base64.RawURLEncoding.EncodeToString(body)
	return &contract.ModelQuotaRuntime{Endpoint: a.endpoint, RequestTimeoutMs: a.requestTimeout.Milliseconds(), Token: payload + "." + a.sign(payload), MaxOutputTokens: value.ModelQuota.MaxOutputTokens}, nil
}

func (a *Authority) sign(payload string) string {
	mac := hmac.New(sha256.New, a.key)
	_, _ = mac.Write([]byte("rank-model-quota-v1\x00" + payload))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func (a *Authority) authorize(token string) (claims, error) {
	var value claims
	payload, signature, ok := strings.Cut(token, ".")
	if !ok || len(token) > 4096 || !hmac.Equal([]byte(signature), []byte(a.sign(payload))) {
		return value, ErrInactive
	}
	body, err := base64.RawURLEncoding.DecodeString(payload)
	if err != nil || json.Unmarshal(body, &value) != nil || value.ID == "" || value.Attempt < 1 || value.ExpiresAt <= time.Now().UnixMilli() {
		return value, ErrInactive
	}
	return value, nil
}

// Handler authorizes only permit acquisition and usage reports. It does not
// accept the ordinary service token and cannot read or alter model credentials.
func (a *Authority) Handler(repo Repository) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Content-Type", "application/json")
		fail := func(status int, code string) {
			w.WriteHeader(status)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": code})
		}
		if r.Method != http.MethodPost || r.URL.Path != "/v1/model-permits" {
			fail(404, "not_found")
			return
		}
		token, hasBearer := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		if !hasBearer {
			fail(401, "invalid_model_permit_identity")
			return
		}
		value, err := a.authorize(token)
		if err != nil {
			fail(401, "invalid_model_permit_identity")
			return
		}
		var input struct {
			Sequence     int    `json:"sequence"`
			InputTokens  int64  `json:"inputTokens"`
			ActualTokens *int64 `json:"actualTokens,omitempty"`
		}
		decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096))
		decoder.DisallowUnknownFields()
		if decoder.Decode(&input) != nil {
			fail(400, "invalid_model_permit_request")
			return
		}
		if decoder.Decode(new(any)) != io.EOF {
			fail(400, "invalid_model_permit_request")
			return
		}
		var result any
		if input.ActualTokens != nil {
			err = repo.ReconcileModelPermit(r.Context(), value.ID, value.Attempt, input.Sequence, *input.ActualTokens)
			result = map[string]bool{"recorded": err == nil}
		} else {
			result, err = repo.ReserveModelPermit(r.Context(), value.ID, value.Attempt, contract.ModelPermitRequest{Sequence: input.Sequence, InputTokens: input.InputTokens}, a.validity)
		}
		switch {
		case errors.Is(err, ErrInvalid), errors.Is(err, ErrTooLarge):
			fail(400, "model_request_exceeds_quota")
			return
		case errors.Is(err, ErrConflict):
			fail(409, "model_permit_conflict")
			return
		case errors.Is(err, ErrInactive):
			fail(403, "model_execution_inactive")
			return
		case err != nil:
			fail(503, "model_quota_unavailable")
			return
		}
		_ = json.NewEncoder(w).Encode(result)
	})
}
