package auth

import (
	"io"
	"m365-copilot2api/internal/outbound"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const testPersonalOID = "11111111-2222-4333-8444-555555555555"

func TestPersonalCredentialsValidation(t *testing.T) {
	p := PersonalCredentials{AccessToken: "opaque-secret", OID: testPersonalOID, ExpiresAt: float64(time.Now().Add(time.Hour).Unix())}
	set, err := p.TokenSet()
	if err != nil || set.TenantID != PersonalChatTenant || set.PersonalLicense != "Free" {
		t.Fatalf("valid credentials: %v", err)
	}
	for _, bad := range []PersonalCredentials{{OID: "bad", AccessToken: "secret"}, {OID: testPersonalOID, LicenseType: "Enterprise", AccessToken: "secret"}, {OID: testPersonalOID, AccessToken: "secret", ExpiresAt: 1}, {OID: testPersonalOID}} {
		if _, err := bad.TokenSet(); err == nil {
			t.Fatal("accepted invalid credentials")
		}
	}
}
func TestPersonalRefreshRotationAndSafeErrors(t *testing.T) {
	for _, rotate := range []bool{false, true} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			r.ParseForm()
			if r.Form.Get("refresh_token") != "old-secret" || r.Header.Get("Origin") != "https://copilot.com" {
				t.Error("missing refresh grant or Origin")
			}
			if rotate {
				w.Write([]byte(`{"access_token":"new","refresh_token":"rotated","expires_in":3600}`))
			} else {
				w.Write([]byte(`{"access_token":"new","expires_in":3600}`))
			}
		}))
		set, err := refreshPersonalAt(server.Client(), server.URL, url.Values{"refresh_token": {"old-secret"}}, testPersonalOID)
		server.Close()
		want := "old-secret"
		if rotate {
			want = "rotated"
		}
		if err != nil || set.RefreshToken != want || set.HomeOID != testPersonalOID || set.TenantID != PersonalChatTenant {
			t.Fatalf("refresh failed: %v", err)
		}
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(400)
		w.Write([]byte(`{"error":"invalid_grant","error_description":"AADSTS70000 old-secret"}`))
	}))
	defer server.Close()
	_, err := refreshPersonalAt(server.Client(), server.URL, url.Values{}, testPersonalOID)
	if err == nil || strings.Contains(err.Error(), "old-secret") {
		t.Fatalf("unsafe error: %v", err)
	}
}
func TestPersonalStorePersistsProfileAndSeparatesOrganization(t *testing.T) {
	path := filepath.Join(t.TempDir(), "accounts.json")
	s, _ := OpenStore(path)
	for _, tenant := range []string{"org-tenant", PersonalChatTenant} {
		_, err := s.Upsert(TokenSet{AccessToken: "secret", RefreshToken: "refresh", HomeOID: tenant, Email: "same@example.test", TenantID: tenant, PersonalLicense: "Premium", ExpiresAt: time.Now().Add(time.Hour)})
		if err != nil {
			t.Fatal(err)
		}
	}
	s, err := OpenStore(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(s.List()) != 2 {
		t.Fatal("personal and organization identities merged")
	}
	a, _ := s.Get(PersonalChatTenant)
	if a.PersonalLicense != "Premium" {
		t.Fatal("profile lost on reload")
	}
}

type personalRoundTripper func(*http.Request) (*http.Response, error)

func (f personalRoundTripper) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestPersonalStoreAutoRefreshUsesConsumerGrant(t *testing.T) {
	t.Setenv("M365_BROWSER_SCOPE", "organization-only-scope")
	client := outbound.HTTPClient()
	original := client.Transport
	defer func() { client.Transport = original }()
	called := false
	client.Transport = personalRoundTripper(func(r *http.Request) (*http.Response, error) {
		called = true
		r.ParseForm()
		if r.URL.Host != "login.microsoftonline.com" || !strings.Contains(r.URL.Path, PersonalOAuthTenant) || strings.Contains(r.URL.Path, PersonalChatTenant) {
			t.Error("refresh used wrong tenant")
		}
		if r.Form.Get("scope") != personalScope || r.Form.Get("client_id") != DefaultClientID || r.Form.Get("grant_type") != "refresh_token" || r.Form.Get("brk_redirect_uri") != "https://copilot.com/spalanding" || r.Form.Get("X-AnchorMailbox") != "Oid:"+testPersonalOID+"@"+PersonalOAuthTenant {
			t.Error("incorrect consumer broker grant")
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"access_token":"renewed-access","refresh_token":"rotated-refresh","expires_in":3600}`)), Header: make(http.Header)}, nil
	})
	path := filepath.Join(t.TempDir(), "accounts.json")
	store, _ := OpenStore(path)
	_, err := store.Upsert(TokenSet{HomeOID: testPersonalOID, TenantID: PersonalChatTenant, PersonalLicense: "Premium", AccessToken: "expired", RefreshToken: "old", ExpiresAt: time.Now().Add(-time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	got, err := store.EnsureValid(testPersonalOID)
	if err != nil || !called || got.AccessToken != "renewed-access" || got.PersonalLicense != "Premium" || got.RefreshToken != "rotated-refresh" {
		t.Fatalf("automatic refresh lost account state: %v", err)
	}
	reopened, err := OpenStore(path)
	if err != nil {
		t.Fatal(err)
	}
	saved, _ := reopened.Get(testPersonalOID)
	if saved.RefreshToken != "rotated-refresh" || saved.PersonalLicense != "Premium" {
		t.Fatal("refresh rotation or profile not persisted")
	}
}
