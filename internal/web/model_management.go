package web

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"m365-copilot2api/internal/auth"
	"m365-copilot2api/internal/chathub"
)

type modelScopeKey struct{}
type modelRouteKey struct{}

func validModelScenario(s string) bool {
	switch s {
	case "", "OfficeWebIncludedCopilot", "OfficeWebPaidCopilot", "OfficeWebFreeConsumerCopilot", "OfficeWebPremiumConsumerCopilot":
		return true
	}
	return false
}
func modelRouteRequest(ctx context.Context, request chathub.Request) chathub.Request {
	if m, ok := ctx.Value(modelRouteKey{}).(modelMapping); ok {
		request.RouteScenario = m.Scenario
	}
	return request
}

func normalizeModelScope(scope string) string {
	if scope == "" {
		return "all"
	}
	return scope
}
func validModelScope(scope string) bool {
	switch normalizeModelScope(scope) {
	case "all", "organization", "personal-free", "personal-premium":
		return true
	}
	return false
}
func accountModelScope(a auth.AccountToken) string {
	if a.TID != auth.PersonalChatTenant {
		return "organization"
	}
	if a.PersonalLicense == "Premium" {
		return "personal-premium"
	}
	return "personal-free"
}
func modelScopeMatches(scope string, a auth.AccountToken) bool {
	return normalizeModelScope(scope) == "all" || scope == accountModelScope(a)
}
func withModelScope(ctx context.Context, model string) context.Context {
	m, ok := configuredModelMapping(model, currentSettings().ModelMappings)
	if !ok {
		return ctx
	}
	ctx = context.WithValue(ctx, modelRouteKey{}, m)
	return context.WithValue(ctx, modelScopeKey{}, normalizeModelScope(m.AccountScope))
}
func scopeFromContext(ctx context.Context) string {
	scope, _ := ctx.Value(modelScopeKey{}).(string)
	return normalizeModelScope(scope)
}
func (s *Server) checkAccountModelScope(ctx context.Context, id string) error {
	if scopeFromContext(ctx) == "all" || s.tokens == nil {
		return nil
	}
	a, ok := s.tokens.Get(id)
	if !ok || !modelScopeMatches(scopeFromContext(ctx), a) {
		return fmt.Errorf("selected account does not match the configured model account scope")
	}
	return nil
}
func (s *Server) scopedModelCatalog() []map[string]any {
	rows := modelCatalog()
	if s.tokens == nil {
		return rows
	}
	accounts := s.tokens.List()
	out := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		scope, _ := row["account_scope"].(string)
		if scope == "all" {
			out = append(out, row)
			continue
		}
		for _, a := range accounts {
			if !a.ScheduleDisabled && modelScopeMatches(scope, a) {
				out = append(out, row)
				break
			}
		}
	}
	return out
}

type modelProbeResult struct {
	FailureReason    string    `json:"failureReason,omitempty"`
	AccountID        string    `json:"accountId"`
	AccountScope     string    `json:"accountScope"`
	Tone             string    `json:"tone"`
	Scenario         string    `json:"scenario,omitempty"`
	Status           string    `json:"status"`
	IdentityVerified bool      `json:"identityVerified"`
	CheckedAt        time.Time `json:"checkedAt"`
	LatencyMS        int64     `json:"latencyMs"`
}

func (s *Server) modelDiscovery(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if r.Method != "GET" {
		writeOpenAIError(w, 405, "invalid_request_error", "method not allowed")
		return
	}
	probes := []modelProbeResult{}
	s.modelProbes.Range(func(_, v any) bool { probes = append(probes, v.(modelProbeResult)); return true })
	jsonOut(w, map[string]any{"discovery": modelDiscoveryInfo(), "probes": probes, "upstreamTones": liveUpstreamTones()})
}
func (s *Server) probeModel(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if r.Method != "POST" {
		writeOpenAIError(w, 405, "invalid_request_error", "method not allowed")
		return
	}
	if !s.validAdminSession(r) {
		writeOpenAIError(w, 401, "auth_error", "administrator login required")
		return
	}
	var b struct {
		AccountID string `json:"accountId"`
		Tone      string `json:"tone"`
		Scenario  string `json:"scenario"`
	}
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 2048)).Decode(&b) != nil || b.AccountID == "" || (!validToneIdentifier(b.Tone) || !validModelScenario(b.Scenario)) {
		writeOpenAIError(w, 400, "invalid_request_error", "accountId and a valid tone identifier required")
		return
	}
	a, err := s.resolveAccountCtx(r.Context(), b.AccountID)
	if err != nil {
		writeUpstreamError(w, err)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()
	start := time.Now()
	cfg := s.settings.get()
	// Candidate rejection is route-specific evidence, not evidence that the
	// entire account credential is unhealthy. Keep normal concurrency limits,
	// but do not feed exploratory failures into account/global circuit breakers.
	release, err := s.accountConcurrency.Acquire(ctx, a.ID)
	if err != nil {
		writeUpstreamError(w, err)
		return
	}
	defer release()
	res, err := s.accountClient(a.ID).Chat(ctx, chathub.Account{AccessToken: a.AccessToken, OID: a.OID, TID: a.TID, PersonalLicense: a.PersonalLicense}, chathub.Request{Text: "Reply only OK.", Tone: b.Tone, RouteScenario: b.Scenario, LicenseType: cfg.LicenseType, Scenario: cfg.Scenario})
	status := "request_failed"
	if err == nil && strings.EqualFold(strings.Trim(strings.TrimSpace(res.Text), ".!。！"), "OK") {
		status = "request_ok_identity_unconfirmed"
	}
	result := modelProbeResult{AccountID: a.ID, AccountScope: accountModelScope(a), Tone: b.Tone, Scenario: b.Scenario, Status: status, CheckedAt: time.Now(), LatencyMS: time.Since(start).Milliseconds()}
	if status == "request_failed" {
		if err != nil {
			result.FailureReason = modelProbeFailureReason(err)
		} else {
			result.FailureReason = "Unexpected or empty reply; route availability is unconfirmed."
		}
	}
	s.modelProbes.Store(a.ID+"\n"+b.Tone+"\n"+b.Scenario, result)
	// Do not report a model's self-description as identity evidence. Most ChatHub
	// replies provide no trustworthy, documented resolved-model identifier.
	jsonOut(w, map[string]any{"result": result, "note": "Request acceptance does not prove resolved model identity; Microsoft may route or fall back."})
}

func modelProbeFailureReason(err error) string {
	switch {
	case errors.Is(err, chathub.ErrRegionUnavailable):
		return "Microsoft reports Copilot is unavailable in this region. Model availability is unconfirmed."
	case errors.Is(err, chathub.ErrEmptyCompletion):
		return "No completion received. This model route is unavailable or unsupported in the current context."
	case errors.Is(err, chathub.ErrRateLimitNotice):
		return "Microsoft is rate limiting this account. Model availability is unconfirmed; retry later."
	case errors.Is(err, context.DeadlineExceeded), errors.Is(err, chathub.ErrFirstTokenTimeout):
		return "The request timed out. Model availability is unconfirmed; retry later."
	default:
		return fmt.Sprintf("Upstream request failed (%s). Model availability is unconfirmed.", ClassifyError(err))
	}
}
