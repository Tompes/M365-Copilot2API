package web

import (
	"context"
	"encoding/json"
	"m365-copilot2api/internal/auth"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type fakePersonalDevice struct {
	polls  int
	status string
}

func (f *fakePersonalDevice) Start(context.Context) (auth.DeviceCode, error) {
	return auth.DeviceCode{DeviceCode: "never-return-this", UserCode: "ABC123", VerificationURI: "https://microsoft.com/devicelogin", ExpiresAt: time.Now().Add(time.Minute), Interval: 5}, nil
}
func (f *fakePersonalDevice) Poll(context.Context, string) (auth.PersonalCredentials, string, error) {
	f.polls++
	return auth.PersonalCredentials{}, f.status, nil
}
func TestPersonalDeviceSessionAndLifecycle(t *testing.T) {
	fake := &fakePersonalDevice{status: "slow_down"}
	s := &Server{adminSessions: map[string]time.Time{"one": time.Now().Add(time.Hour), "two": time.Now().Add(time.Hour)}, personalAuthorizer: fake}
	call := func(handler http.HandlerFunc, owner, body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("POST", "/api/auth/personal/device", strings.NewReader(body))
		if owner != "" {
			r.AddCookie(&http.Cookie{Name: "m365_admin_session", Value: owner})
		}
		w := httptest.NewRecorder()
		handler(w, r)
		return w
	}
	if w := call(s.startPersonalDevice, "", `{"license_type":"Free"}`); w.Code != 401 {
		t.Fatal("unauthenticated start")
	}
	w := call(s.startPersonalDevice, "one", `{"license_type":"Free"}`)
	if w.Code != 200 || strings.Contains(w.Body.String(), "never-return-this") {
		t.Fatal("unsafe start response")
	}
	var result struct {
		ID string `json:"id"`
	}
	json.Unmarshal(w.Body.Bytes(), &result)
	body := `{"id":"` + result.ID + `"}`
	if w := call(s.pollPersonalDevice, "two", body); w.Code != 404 {
		t.Fatal("cross-session access")
	}
	call(s.pollPersonalDevice, "one", body)
	if fake.polls != 0 {
		t.Fatal("poll interval bypass")
	}
	f := s.personalDevices.flows[result.ID]
	f.next = time.Now().Add(-time.Second)
	call(s.pollPersonalDevice, "one", body)
	if fake.polls != 1 || f.device.Interval != 10 {
		t.Fatal("slow_down not respected")
	}
	call(s.pollPersonalDevice, "one", body)
	if fake.polls != 1 {
		t.Fatal("poll interval bypass after slowdown")
	}
	call(s.cancelPersonalDevice, "one", body)
	if w := call(s.pollPersonalDevice, "one", body); w.Code != 410 {
		t.Fatal("cancelled flow polled")
	}
	w = call(s.startPersonalDevice, "one", `{"license_type":"Premium"}`)
	if _, ok := s.personalDevices.flows[result.ID]; ok {
		t.Fatal("previous flow not removed")
	}
	json.Unmarshal(w.Body.Bytes(), &result)
	f = s.personalDevices.flows[result.ID]
	f.device.ExpiresAt = time.Now().Add(-time.Second)
	if w := call(s.pollPersonalDevice, "one", `{"id":"`+result.ID+`"}`); w.Code != 410 {
		t.Fatal("expired flow polled")
	}
}
