package auth

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPersonalDeviceProtocol(t *testing.T) {
	state := "authorization_pending"
	oid := "11111111-2222-3333-4444-555555555555"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		if r.Form.Get("client_id") != personalDeviceClientID || r.Header.Get("Origin") != "" {
			t.Error("wrong native client or origin")
		}
		if r.URL.Path == "/devicecode" {
			if r.Form.Get("scope") != "openid profile offline_access" {
				t.Error("unexpected scope")
			}
			fmt.Fprint(w, `{"device_code":"private-device-secret","user_code":"ABCDE1234","verification_uri":"https://untrusted.invalid","expires_in":900,"interval":5}`)
			return
		}
		if r.Form.Get("device_code") != "private-device-secret" || r.Form.Get("grant_type") != "urn:ietf:params:oauth:grant-type:device_code" {
			t.Error("wrong device grant")
		}
		if state != "success" {
			w.WriteHeader(400)
			fmt.Fprintf(w, `{"error":%q,"error_description":"never expose private-device-secret"}`, state)
			return
		}
		claims := base64.RawURLEncoding.EncodeToString([]byte(fmt.Sprintf(`{"aud":%q,"tid":%q,"oid":%q}`, personalDeviceClientID, PersonalOAuthTenant, oid)))
		fmt.Fprintf(w, `{"access_token":"identity-only","refresh_token":"native-refresh","id_token":"header.%s.signature"}`, claims)
	}))
	defer srv.Close()
	c := PersonalDeviceClient{Client: srv.Client(), BaseURL: srv.URL}
	d, err := c.Start(context.Background())
	if err != nil || d.VerificationURI != "https://microsoft.com/devicelogin" {
		t.Fatalf("start: %v", err)
	}
	for _, status := range []string{"authorization_pending", "slow_down"} {
		state = status
		_, got, err := c.Poll(context.Background(), d.DeviceCode)
		if err != nil || got != status {
			t.Fatalf("pending %s: %v", status, err)
		}
	}
	state = "authorization_declined"
	_, _, err = c.Poll(context.Background(), d.DeviceCode)
	if err == nil || strings.Contains(err.Error(), "private-device-secret") {
		t.Fatal("unsafe terminal error")
	}
	state = "success"
	p, status, err := c.Poll(context.Background(), d.DeviceCode)
	if err != nil || status != "authorized" || p.OID != oid || !p.NativeRefresh || p.RefreshToken != "native-refresh" || p.AccessToken != "" {
		t.Fatalf("grant not suitable for native chat exchange: %v", err)
	}
}
