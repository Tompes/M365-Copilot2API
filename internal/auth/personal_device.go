package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
	"m365-copilot2api/internal/outbound"
)

const personalDeviceClientID = "d3590ed6-52b3-4102-aeff-aad2292ab01c"
const personalDeviceBase = "https://login.microsoftonline.com/consumers/oauth2/v2.0"

// PersonalDeviceClient is separate from organization device-code settings.
// Device secrets and token responses never leave the backend.
type PersonalDeviceClient struct {
	Client  *http.Client
	BaseURL string
}

func (c PersonalDeviceClient) post(ctx context.Context, path string, form url.Values, dest any) (int, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	base := c.BaseURL
	if base == "" {
		base = personalDeviceBase
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+path, strings.NewReader(form.Encode()))
	if err != nil {
		return 0, fmt.Errorf("personal authorization request failed")
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	client := c.Client
	if client == nil {
		client = outbound.HTTPClient()
	}
	resp, err := client.Do(req)
	if err != nil {
		return 0, fmt.Errorf("personal authorization connection failed; try again")
	}
	defer resp.Body.Close()
	if json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(dest) != nil {
		return resp.StatusCode, fmt.Errorf("invalid personal authorization response")
	}
	return resp.StatusCode, nil
}

func (c PersonalDeviceClient) Start(ctx context.Context) (DeviceCode, error) {
	var d deviceCodeResponse
	status, err := c.post(ctx, "/devicecode", url.Values{"client_id": {personalDeviceClientID}, "scope": {"openid profile offline_access"}}, &d)
	if err != nil {
		return DeviceCode{}, err
	}
	if status != 200 || d.Error != "" {
		return DeviceCode{}, &OAuthError{Code: "personal_device_start_failed", HTTPStatus: status, AADSTS: aadstsCode(d.ErrorDesc)}
	}
	// Consumer device codes use /link; organization codes can use /devicelogin.
	// Preserve the Microsoft-provided destination, with an exact allowlist.
	switch d.VerificationURI {
	case "https://www.microsoft.com/link", "https://microsoft.com/link", "https://microsoft.com/devicelogin", "https://www.microsoft.com/devicelogin":
	default:
		return DeviceCode{}, fmt.Errorf("unexpected Microsoft device verification address")
	}
	if d.DeviceCode == "" || d.UserCode == "" || d.ExpiresIn <= 0 || d.ExpiresIn > 1800 {
		return DeviceCode{}, fmt.Errorf("invalid personal device authorization response")
	}
	interval := d.Interval
	if interval < 5 {
		interval = 5
	}
	if interval > 60 {
		interval = 60
	}
	return DeviceCode{DeviceCode: d.DeviceCode, UserCode: d.UserCode, VerificationURI: d.VerificationURI, Interval: interval, ExpiresIn: d.ExpiresIn, ExpiresAt: time.Now().Add(time.Duration(d.ExpiresIn) * time.Second)}, nil
}

// Poll returns pending/slow_down without treating them as authentication failures.
// The caller must enforce interval, expiry, session ownership and single-flight.
func (c PersonalDeviceClient) Poll(ctx context.Context, code string) (PersonalCredentials, string, error) {
	var tr tokenResponse
	status, err := c.post(ctx, "/token", url.Values{"client_id": {personalDeviceClientID}, "grant_type": {"urn:ietf:params:oauth:grant-type:device_code"}, "device_code": {code}}, &tr)
	if err != nil {
		return PersonalCredentials{}, "", err
	}
	switch tr.Error {
	case "authorization_pending", "slow_down":
		return PersonalCredentials{}, tr.Error, nil
	case "authorization_declined", "access_denied", "expired_token", "bad_verification_code":
		return PersonalCredentials{}, "", &OAuthError{Code: tr.Error, HTTPStatus: status}
	}
	if status != 200 || tr.Error != "" || tr.RefreshToken == "" {
		return PersonalCredentials{}, "", &OAuthError{Code: "personal_device_login_failed", HTTPStatus: status, AADSTS: aadstsCode(tr.ErrorDesc)}
	}
	// Identity comes only from this backend's HTTPS token redemption, never from
	// a client-supplied JWT. The native identity access token is not a chat token.
	claims, err := decodeJWTClaims(tr.IDToken)
	if err != nil || claims["aud"] != personalDeviceClientID || claims["tid"] != PersonalOAuthTenant {
		return PersonalCredentials{}, "", fmt.Errorf("personal authorization returned an unexpected identity")
	}
	if _, err := uuid.Parse(claims["oid"]); err != nil {
		return PersonalCredentials{}, "", fmt.Errorf("personal authorization returned no account identity")
	}
	return PersonalCredentials{RefreshToken: tr.RefreshToken, OID: claims["oid"], NativeRefresh: true}, "authorized", nil
}
