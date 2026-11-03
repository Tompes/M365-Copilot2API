package chathub

import (
	"encoding/json"
	"m365-copilot2api/internal/auth"
	"net/url"
	"strings"
	"testing"
)

func TestPersonalRoutingAndPayload(t *testing.T) {
	for _, license := range []string{"Free", "Premium"} {
		raw, _ := BuildWSURL(Account{AccessToken: "secret", OID: "personal-oid", TID: auth.PersonalChatTenant, PersonalLicense: license}, "session", "conversation", "request", "Starter", "OfficeWebIncludedCopilot")
		u, _ := url.Parse(raw)
		q := u.Query()
		if q.Get("XRoutingParameterSessionKey") != "personal-oid" || q.Get("licenseType") != license || q.Get("scenario") != "OfficeWeb"+license+"ConsumerCopilot" {
			t.Fatal("wrong personal routing")
		}
	}
	raw, _ := BuildWSURL(Account{OID: "org-oid", TID: "org"}, "s", "c", "r", "Starter", "OfficeWebIncludedCopilot")
	u, _ := url.Parse(raw)
	if u.Query().Get("XRoutingParameterSessionKey") != "r" || u.Query().Get("scenario") != "OfficeWebIncludedCopilot" {
		t.Fatal("organization behavior changed")
	}
	for _, personal := range []bool{false, true} {
		payload := chatPayload(Request{personal: personal, Text: "test", SessionID: "session"}, "request", true)
		var frame map[string]any
		json.Unmarshal([]byte(strings.Split(payload, rs)[0]), &frame)
		a := frame["arguments"].([]any)[0].(map[string]any)
		ci := a["clientInfo"].(map[string]any)
		if personal {
			if a["requestId"] != "request" || a["gpts"] == nil || ci["clientPlatform"] != "copilotcom-web" {
				t.Fatal("missing consumer protocol fields")
			}
		} else if a["gpts"] != nil || ci["clientPlatform"] != "mcmcopilot-web" {
			t.Fatal("organization payload changed")
		}
	}
}

func TestPersonalRegionNoticeIsNotSuccessfulChat(t *testing.T) {
	for _, text := range []string{"Copilot isn’t available in your region.", "Copilot isn't available in your region.", "Copilot 在你所在的区域不可用。"} {
		if !personalRegionUnavailable(text) {
			t.Errorf("notice not detected: %q", text)
		}
	}
	for _, text := range []string{"OK", "Why does it say Copilot isn’t available in your region?", "The documentation says: Copilot isn't available in your region."} {
		if personalRegionUnavailable(text) {
			t.Errorf("normal text misclassified: %q", text)
		}
	}
}
