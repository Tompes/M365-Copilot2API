package web

import (
	"context"
	"fmt"
	"io"
	"m365-copilot2api/internal/outbound"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

// Observed in Microsoft's public web client on 2026-11-03. This is a versioned
// fallback, not an account entitlement list or a promise that it is newest.
const observedModelBundle = "https://res.public.onecdn.static.microsoft/midgard/versionless-v2/main.308627c4.js"

var discoveryMu sync.Mutex
var dynamicMu sync.RWMutex
var dynamicTones []string
var dynamicAt time.Time
var dynamicAttempt time.Time
var dynamicSource, dynamicWarning string

func modelDiscoveryInfo() map[string]any {
	dynamicMu.RLock()
	defer dynamicMu.RUnlock()
	return map[string]any{"source": dynamicSource, "syncedAt": dynamicAt, "warning": dynamicWarning, "candidates": append([]string(nil), dynamicTones...), "accountEntitlements": false}
}
func liveUpstreamTones() []string {
	dynamicMu.RLock()
	defer dynamicMu.RUnlock()
	result := append([]string(nil), knownUpstreamTones()...)
	seen := map[string]bool{}
	for _, t := range result {
		seen[t] = true
	}
	for _, t := range dynamicTones {
		if !seen[t] {
			result = append(result, t)
			seen[t] = true
		}
	}
	return result
}
func syncUpstreamTones() { _, _ = discoverModelTones(context.Background(), "") }

func validModelBundleURL(raw string) bool {
	u, err := url.Parse(raw)
	return err == nil && u.Scheme == "https" && u.Host == "res.public.onecdn.static.microsoft" && u.User == nil && u.RawQuery == "" && u.Fragment == "" && regexp.MustCompile(`^/midgard/versionless-v2/main\.[a-f0-9]{8,64}\.js$`).MatchString(u.Path)
}
func extractModelTones(bundle string) []string {
	// Quoted complete enum values only. Avoid partial matches such as Gpt_5_2_.
	re := regexp.MustCompile("[\"'\x60]((?:Gpt|Claude|Grok|Muse)_[A-Za-z0-9]+(?:_[A-Za-z0-9]+)*|[Mm]agic)[\"'\x60]")
	seen := map[string]bool{}
	for _, m := range re.FindAllStringSubmatch(bundle, -1) {
		seen[m[1]] = true
	}
	result := make([]string, 0, len(seen))
	for t := range seen {
		result = append(result, t)
	}
	sort.Strings(result)
	return result
}
func discoverModelTones(ctx context.Context, source string) ([]string, error) {
	discoveryMu.Lock()
	defer discoveryMu.Unlock()
	if source != "" && !validModelBundleURL(source) {
		return nil, fmt.Errorf("source must be a Microsoft public main.<hash>.js bundle URL")
	}
	ctx, cancel := context.WithTimeout(ctx, 40*time.Second)
	defer cancel()
	client := *outbound.HTTPClient()
	client.Timeout = 30 * time.Second
	client.CheckRedirect = func(req *http.Request, via []*http.Request) error { return http.ErrUseLastResponse }
	get := func(raw string, limit int64) (string, error) {
		req, err := http.NewRequestWithContext(ctx, "GET", raw, nil)
		if err != nil {
			return "", fmt.Errorf("invalid discovery address")
		}
		resp, err := client.Do(req)
		if err != nil {
			return "", fmt.Errorf("Microsoft model discovery connection failed")
		}
		defer resp.Body.Close()
		if resp.StatusCode != 200 {
			return "", fmt.Errorf("Microsoft model discovery HTTP %d", resp.StatusCode)
		}
		b, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
		if err != nil || int64(len(b)) > limit {
			return "", fmt.Errorf("Microsoft model discovery response invalid or too large")
		}
		return string(b), nil
	}
	warning := ""
	if source == "" {
		page, err := get("https://m365.cloud.microsoft/chat", 3<<20)
		if err == nil {
			name := regexp.MustCompile(`main\.[a-f0-9]{8,64}\.js`).FindString(page)
			if name != "" {
				source = "https://res.public.onecdn.static.microsoft/midgard/versionless-v2/" + name
			}
		}
		if source == "" {
			source = observedModelBundle
			warning = "Public page requires sign-in. Using a versioned bundle observed on 2026-11-03; it may not be the latest. Paste a newer Microsoft bundle URL to update."
		}
	}
	bundle, err := get(source, 12<<20)
	var tones []string
	if err == nil {
		tones = extractModelTones(bundle)
		if len(tones) == 0 {
			err = fmt.Errorf("no candidate model identifiers found; previous results retained")
		}
	}
	dynamicMu.Lock()
	defer dynamicMu.Unlock()
	dynamicAttempt = time.Now()
	if err != nil {
		dynamicWarning = err.Error()
		return nil, err
	}
	dynamicTones = tones
	dynamicAt = time.Now()
	dynamicSource = source
	dynamicWarning = strings.TrimSpace(warning)
	return append([]string(nil), tones...), nil
}
