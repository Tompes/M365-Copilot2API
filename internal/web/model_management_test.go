package web

import (
	"context"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"m365-copilot2api/internal/auth"
	"m365-copilot2api/internal/chathub"
)

func TestDiscoverModelIdentifierParsingAndSourceSafety(t *testing.T) {
	got := extractModelTones("x=`Gpt_61_Sol_Reasoning`;y=\"Claude_Opus\";z='Grok_Reasoning';p=`Muse_Spark`;bad=`Gpt_5_2_`;fragment=Gpt_5_4_Chat;a=`magic`;duplicate=`Claude_Opus`")
	want := []string{"Claude_Opus", "Gpt_61_Sol_Reasoning", "Grok_Reasoning", "Muse_Spark", "magic"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v want %v", got, want)
	}
	if !validModelBundleURL(observedModelBundle) {
		t.Fatal("official source rejected")
	}
	for _, u := range []string{"http://127.0.0.1/a.js", "https://res.public.onecdn.static.microsoft.evil.invalid/midgard/versionless-v2/main.308627c4.js", observedModelBundle + "?token=secret", "https://res.public.onecdn.static.microsoft/other.js"} {
		if validModelBundleURL(u) {
			t.Fatalf("unsafe source accepted %s", u)
		}
	}
	if _, err := discoverModelTones(context.Background(), "http://127.0.0.1/"); err == nil {
		t.Fatal("unsafe source fetched")
	}
}
func TestCustomModelPublicationAndAccountScopes(t *testing.T) {
	cfg := defaultRuntimeSettings()
	cfg.HideBuiltinModels = true
	cfg.ModelMappings = []modelMapping{
		{PublicModel: "copilot-auto", UpstreamTone: "Magic", DisplayName: "Automatic", DefaultReasoningLevel: "none", RouteKind: "mode"},
		{PublicModel: "premium-candidate", UpstreamTone: "New_Model_123", DisplayName: "Candidate", DefaultReasoningLevel: "none", AccountScope: "personal-premium", Scenario: "OfficeWebPremiumConsumerCopilot"},
		{PublicModel: "org-candidate", UpstreamTone: "Claude_Opus", DisplayName: "Org candidate", DefaultReasoningLevel: "none", AccountScope: "organization"},
		{PublicModel: "not-published", UpstreamTone: "Other_Model", DisplayName: "Disabled", DefaultReasoningLevel: "none", Disabled: true},
	}
	if err := validateSettings(cfg); err != nil {
		t.Fatal(err)
	}
	original := openSettingsStore
	openSettingsStore = func() *settingsStore { return &settingsStore{v: cfg} }
	t.Cleanup(func() { openSettingsStore = original })
	store, err := auth.OpenStore(filepath.Join(t.TempDir(), "accounts.json"))
	if err != nil {
		t.Fatal(err)
	}
	a, err := store.Upsert(auth.TokenSet{AccessToken: "fake-access", HomeOID: "11111111-1111-1111-1111-111111111111", TenantID: auth.PersonalChatTenant, PersonalLicense: "Premium", ExpiresAt: time.Now().Add(time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	s := &Server{tokens: store}
	rows := s.scopedModelCatalog()
	if len(rows) != 2 {
		t.Fatalf("wrong scoped catalog: %+v", rows)
	}
	for _, row := range rows {
		if row["identity_verified"] != false {
			t.Fatal("identity evidence fabricated")
		}
	}
	if tone, err := reasoningTone("premium-candidate", "high"); err != nil || tone != "New_Model_123" {
		t.Fatal("custom tone changed")
	}
	if _, err := reasoningTone("not-published", ""); err == nil {
		t.Fatal("disabled route usable")
	}
	if err := s.checkAccountModelScope(withModelScope(context.Background(), "org-candidate"), a.ID); err == nil {
		t.Fatal("cross-scope route allowed")
	}
	if err := s.checkAccountModelScope(withModelScope(context.Background(), "premium-candidate"), a.ID); err != nil {
		t.Fatal(err)
	}
	req := modelRouteRequest(withModelScope(context.Background(), "premium-candidate"), chathub.Request{})
	if req.RouteScenario != "OfficeWebPremiumConsumerCopilot" {
		t.Fatal("per-model scenario lost")
	}
	if _, err := reasoningTone("gpt-5.4", ""); err == nil {
		t.Fatal("hidden unconfigured model callable")
	}
	cfg.ModelMappings[1].AccountScope = "invalid"
	if validateSettings(cfg) == nil {
		t.Fatal("invalid scope accepted")
	}
}
func TestModelProbeRequiresAdminAndValidIdentifier(t *testing.T) {
	s := &Server{}
	r := httptest.NewRequest("POST", "/api/admin/models/probe", strings.NewReader(`{"tone":"secret-access"}`))
	w := httptest.NewRecorder()
	s.probeModel(w, r)
	if w.Code != 401 || strings.Contains(w.Body.String(), "secret-access") {
		t.Fatal("unsafe unauthenticated probe")
	}
}

func TestModelProbeKeepsRegionFailureSeparateFromEntitlement(t *testing.T) {
	reason := modelProbeFailureReason(chathub.ErrRegionUnavailable)
	if !strings.Contains(reason, "region") || !strings.Contains(reason, "unconfirmed") {
		t.Fatal("region failure misrepresented as entitlement")
	}
}
