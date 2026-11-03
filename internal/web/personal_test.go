package web

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestPersonalImportRequiresAdminAndRejectsBadFiles(t *testing.T) {
	s := &Server{adminSessions: map[string]time.Time{"admin": time.Now().Add(time.Hour)}}
	for _, tc := range []struct {
		body string
		auth bool
		want int
	}{{`{"access_token":"do-not-echo"}`, false, 401}, {`{`, true, 400}, {`{} {}`, true, 400}, {`{"oid":"invalid","access_token":"do-not-echo"}`, true, 400}, {strings.Repeat("x", 65537), true, 400}} {
		r := httptest.NewRequest(http.MethodPost, "/api/auth/personal/import", strings.NewReader(tc.body))
		if tc.auth {
			r.AddCookie(&http.Cookie{Name: "m365_admin_session", Value: "admin"})
		}
		w := httptest.NewRecorder()
		s.importPersonalAccount(w, r)
		if w.Code != tc.want || strings.Contains(w.Body.String(), "do-not-echo") {
			t.Fatalf("unsafe import response: %d %s", w.Code, w.Body.String())
		}
	}
}
