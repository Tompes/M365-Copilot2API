package web

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"net/http"
	"sync"
	"time"

	"github.com/google/uuid"
	"m365-copilot2api/internal/auth"
)

type personalDeviceAuthorizer interface {
	Start(context.Context) (auth.DeviceCode, error)
	Poll(context.Context, string) (auth.PersonalCredentials, string, error)
}
type personalDeviceStore struct {
	mu    sync.Mutex
	flows map[string]*personalDeviceFlow
}
type personalDeviceFlow struct {
	mu        sync.Mutex
	owner     [32]byte
	device    auth.DeviceCode
	plan      string
	next      time.Time
	ctx       context.Context
	cancel    context.CancelFunc
	finished  bool
	cleanupAt time.Time
}

func (s *Server) deviceAuthorizer() personalDeviceAuthorizer {
	if s.personalAuthorizer != nil {
		return s.personalAuthorizer
	}
	return auth.PersonalDeviceClient{}
}
func personalDeviceOwner(r *http.Request) [32]byte {
	c, _ := r.Cookie("m365_admin_session")
	if c == nil {
		return [32]byte{}
	}
	return sha256.Sum256([]byte(c.Value))
}
func (s *Server) personalDeviceRequest(w http.ResponseWriter, r *http.Request, dest any) bool {
	w.Header().Set("Cache-Control", "no-store")
	if r.Method != http.MethodPost {
		writeOpenAIError(w, 405, "invalid_request_error", "method not allowed")
		return false
	}
	if !s.validAdminSession(r) {
		writeOpenAIError(w, 401, "auth_error", "administrator login required")
		return false
	}
	d := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024))
	if d.Decode(dest) != nil {
		writeOpenAIError(w, 400, "invalid_request_error", "invalid authorization request")
		return false
	}
	return true
}
func (s *Server) startPersonalDevice(w http.ResponseWriter, r *http.Request) {
	var p struct {
		Plan string `json:"license_type"`
	}
	if !s.personalDeviceRequest(w, r, &p) {
		return
	}
	if p.Plan != "Free" && p.Plan != "Premium" {
		writeOpenAIError(w, 400, "invalid_request_error", "license_type must be Free or Premium")
		return
	}
	owner := personalDeviceOwner(r)
	// Reserve before calling Microsoft, bounding concurrent starts too.
	store := &s.personalDevices
	store.mu.Lock()
	if store.flows == nil {
		store.flows = map[string]*personalDeviceFlow{}
	}
	for id, f := range store.flows {
		if time.Now().After(f.cleanupAt) || f.owner == owner {
			f.cancel()
			delete(store.flows, id)
		}
	}
	if len(store.flows) >= 32 {
		store.mu.Unlock()
		writeOpenAIError(w, 429, "rate_limit_error", "too many pending authorizations")
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	f := &personalDeviceFlow{owner: owner, plan: p.Plan, cleanupAt: time.Now().Add(30 * time.Minute), ctx: ctx, cancel: cancel, device: auth.DeviceCode{ExpiresAt: time.Now().Add(30 * time.Minute)}}
	id := uuid.NewString()
	store.flows[id] = f
	f.mu.Lock()
	store.mu.Unlock()
	defer f.mu.Unlock()
	d, err := s.deviceAuthorizer().Start(ctx)
	if err != nil || ctx.Err() != nil {
		cancel()
		store.mu.Lock()
		delete(store.flows, id)
		store.mu.Unlock()
		writeOpenAIError(w, 502, "personal_auth_error", "Could not start Microsoft authorization. Try again.")
		return
	}
	f.device = d
	f.next = time.Now().Add(time.Duration(d.Interval) * time.Second)
	jsonOut(w, map[string]any{"id": id, "userCode": d.UserCode, "verificationUri": d.VerificationURI, "expiresAt": d.ExpiresAt, "interval": d.Interval})
}
func (s *Server) findPersonalDevice(w http.ResponseWriter, r *http.Request) (*personalDeviceFlow, bool) {
	var p struct {
		ID string `json:"id"`
	}
	if !s.personalDeviceRequest(w, r, &p) {
		return nil, false
	}
	store := &s.personalDevices
	store.mu.Lock()
	f := store.flows[p.ID]
	if f == nil || f.owner != personalDeviceOwner(r) {
		store.mu.Unlock()
		writeOpenAIError(w, 404, "personal_auth_error", "Authorization not found. Start again.")
		return nil, false
	}
	store.mu.Unlock()
	return f, true
}
func (s *Server) cancelPersonalDevice(w http.ResponseWriter, r *http.Request) {
	f, ok := s.findPersonalDevice(w, r)
	if !ok {
		return
	}
	f.cancel()
	jsonOut(w, map[string]string{"status": "cancelled"})
}
func (s *Server) pollPersonalDevice(w http.ResponseWriter, r *http.Request) {
	f, ok := s.findPersonalDevice(w, r)
	if !ok {
		return
	}
	if !f.mu.TryLock() {
		jsonOut(w, map[string]any{"status": "pending", "interval": 5})
		return
	}
	defer f.mu.Unlock()
	if f.ctx.Err() != nil || time.Now().After(f.device.ExpiresAt) {
		f.device.DeviceCode = ""
		writeOpenAIError(w, 410, "personal_auth_expired", "Authorization expired or cancelled. Start again.")
		return
	}
	if f.finished {
		jsonOut(w, map[string]string{"status": "verified"})
		return
	}
	pending := func() { jsonOut(w, map[string]any{"status": "pending", "interval": f.device.Interval}) }
	if time.Now().Before(f.next) {
		pending()
		return
	}
	// Schedule after completion as well, so slow requests cannot produce bursts.
	p, status, err := s.deviceAuthorizer().Poll(f.ctx, f.device.DeviceCode)
	if status == "slow_down" {
		f.device.Interval += 5
	}
	f.next = time.Now().Add(time.Duration(f.device.Interval) * time.Second)
	if err != nil {
		f.cancel()
		f.device.DeviceCode = ""
		writeOpenAIError(w, 400, "personal_auth_error", err.Error())
		return
	}
	if status != "authorized" {
		pending()
		return
	}
	f.device.DeviceCode = ""
	p.LicenseType = f.plan
	tok, err := p.TokenSet()
	if err != nil {
		f.cancel()
		writeOpenAIError(w, 502, "personal_auth_error", err.Error())
		return
	}
	if f.ctx.Err() != nil {
		writeOpenAIError(w, 410, "personal_auth_expired", "Authorization cancelled. Start again.")
		return
	}
	// Chat verification is tied to cancellation too. The browser receives no tokens.
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	stop := context.AfterFunc(f.ctx, cancel)
	defer stop()
	if s.saveVerifiedPersonal(w, r.WithContext(ctx), tok) {
		f.finished = true
	} else {
		f.cancel()
	}
	// Retain only the completion marker so a lost response is safe to poll again.
}
