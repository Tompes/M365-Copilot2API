package web

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"m365-copilot2api/internal/auth"
	"m365-copilot2api/internal/chathub"
	"net/http"
	"strings"
	"time"
)

func accountType(a auth.AccountToken) string {
	if a.TID == auth.PersonalChatTenant {
		return "personal"
	}
	return "organization"
}

// Import is admin-only and never returns submitted credentials. A real chat
// probe must succeed before persisting; OAuth success alone is insufficient.
func (s *Server) importPersonalAccount(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if r.Method != http.MethodPost {
		writeOpenAIError(w, 405, "invalid_request_error", "method not allowed")
		return
	}
	if !s.validAdminSession(r) {
		writeOpenAIError(w, 401, "auth_error", "administrator login required")
		return
	}
	var p auth.PersonalCredentials
	d := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10))
	if err := d.Decode(&p); err != nil {
		writeOpenAIError(w, 400, "invalid_request_error", "invalid personal credential file")
		return
	}
	if d.Decode(&struct{}{}) != io.EOF {
		writeOpenAIError(w, 400, "invalid_request_error", "expected one JSON object")
		return
	}
	tok, err := p.TokenSet()
	if err != nil {
		writeOpenAIError(w, 400, "personal_auth_error", err.Error())
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()
	result, err := s.chat.Chat(ctx, chathub.Account{AccessToken: tok.AccessToken, OID: tok.HomeOID, TID: tok.TenantID, PersonalLicense: tok.PersonalLicense}, chathub.Request{Text: "Reply only OK."})
	if errors.Is(err, chathub.ErrRegionUnavailable) {
		writeOpenAIError(w, http.StatusBadGateway, "personal_region_unavailable", "Microsoft reports Copilot is unavailable in the current region. No account was saved.")
		return
	}
	if err != nil || !strings.EqualFold(strings.Trim(strings.TrimSpace(result.Text), ".!。！"), "OK") {
		writeOpenAIError(w, 502, "personal_chat_error", "Personal chat verification failed. Check the selected plan and sign in again; no account was saved.")
		return
	}
	tok.DisplayName = "Personal Copilot (" + tok.PersonalLicense + ")"
	acc, err := s.tokens.Upsert(tok)
	if err != nil {
		writeOpenAIError(w, 500, "internal_error", "could not save personal account")
		return
	}
	jsonOut(w, map[string]any{"status": "verified", "account": map[string]any{"id": acc.ID, "accountType": "personal", "personalLicense": acc.PersonalLicense, "expiresAt": acc.ExpiresAt, "autoRefresh": acc.RefreshToken != "", "nativeRefresh": acc.PersonalNative}})
}
