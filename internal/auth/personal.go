package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/google/uuid"
	"io"
	"m365-copilot2api/internal/outbound"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Microsoft uses different tenant IDs for consumer OAuth and ChatHub routing.
const PersonalOAuthTenant = "9188040d-6c67-4c5b-b112-36a304b66dad"
const PersonalChatTenant = "84df9e7f-e9f6-40af-b435-aaaaaaaaaaaa"
const personalScope = "https://substrate.office.com/sydney/v2/.default openid profile offline_access"

// PersonalCredentials is the local sign-in helper's export format. It also
// accepts Windows-Copilot-API's token.json; a refresh obtains the chat resource.
type PersonalCredentials struct {
	AccessToken  string  `json:"access_token"`
	RefreshToken string  `json:"refresh_token"`
	OID          string  `json:"oid"`
	ExpiresAt    float64 `json:"expires_at"`
	LicenseType  string  `json:"license_type"`
}

func (p PersonalCredentials) TokenSet() (TokenSet, error) {
	if _, err := uuid.Parse(p.OID); err != nil {
		return TokenSet{}, fmt.Errorf("personal credentials require a valid oid")
	}
	if p.LicenseType == "" {
		p.LicenseType = "Free"
	}
	if p.LicenseType != "Free" && p.LicenseType != "Premium" {
		return TokenSet{}, fmt.Errorf("license_type must be Free or Premium")
	}
	if p.AccessToken == "" && p.RefreshToken == "" {
		return TokenSet{}, fmt.Errorf("access_token or refresh_token required")
	}
	t := TokenSet{AccessToken: p.AccessToken, RefreshToken: p.RefreshToken, HomeOID: p.OID, TenantID: PersonalChatTenant, PersonalLicense: p.LicenseType, ExpiresAt: time.Unix(int64(p.ExpiresAt), 0)}
	if p.RefreshToken != "" {
		renewed, err := RefreshPersonal(p.RefreshToken, p.OID)
		if err != nil {
			return TokenSet{}, err
		}
		renewed.PersonalLicense = p.LicenseType
		return renewed, nil
	}
	if !t.Valid() || t.ExpiresAt.After(time.Now().Add(24*time.Hour)) {
		return TokenSet{}, fmt.Errorf("access token expiry must be within the next 24 hours; sign in again")
	}
	return t, nil
}

// RefreshPersonal mirrors the consumer broker grant. Enterprise scope/tenant
// substitutions do not produce a usable personal ChatHub token.
func RefreshPersonal(refreshToken, oid string) (TokenSet, error) {
	form := url.Values{
		"client_id": {DefaultClientID}, "grant_type": {"refresh_token"},
		"refresh_token": {refreshToken}, "scope": {personalScope},
		"redirect_uri": {"brk-multihub://outlook.office.com"}, "client_info": {"1"},
		"brk_client_id":    {"4765445b-32c6-49b0-83e6-1d93765276ca"},
		"brk_redirect_uri": {"https://copilot.com/spalanding"},
		"X-AnchorMailbox":  {"Oid:" + oid + "@" + PersonalOAuthTenant},
	}
	endpoint := "https://login.microsoftonline.com/" + PersonalOAuthTenant + "/oauth2/v2.0/token?brk_client_id=4765445b-32c6-49b0-83e6-1d93765276ca"
	return refreshPersonalAt(outbound.HTTPClient(), endpoint, form, oid)
}

func refreshPersonalAt(client *http.Client, endpoint string, form url.Values, oid string) (TokenSet, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return TokenSet{}, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Origin", "https://copilot.com")
	resp, err := client.Do(req)
	if err != nil {
		return TokenSet{}, fmt.Errorf("personal token refresh transport failed")
	}
	defer resp.Body.Close()
	var tr tokenResponse
	if err = json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&tr); err != nil {
		return TokenSet{}, fmt.Errorf("invalid personal token response")
	}
	if resp.StatusCode != http.StatusOK || tr.Error != "" || tr.AccessToken == "" || tr.ExpiresIn <= 0 {
		return TokenSet{}, &OAuthError{Code: "personal_refresh_failed", AADSTS: aadstsCode(tr.ErrorDesc), HTTPStatus: resp.StatusCode}
	}
	return TokenSet{AccessToken: tr.AccessToken, RefreshToken: firstNonEmpty(tr.RefreshToken, form.Get("refresh_token")), ExpiresIn: tr.ExpiresIn, ExpiresAt: time.Now().Add(time.Duration(tr.ExpiresIn) * time.Second), HomeOID: oid, TenantID: PersonalChatTenant}, nil
}
