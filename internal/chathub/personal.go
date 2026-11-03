package chathub

import "strings"

// Consumer availability notices may arrive as successful text completions.
// Match the complete notice so ordinary discussion of availability still works.
func personalRegionUnavailable(text string) bool {
	normalized := strings.ToLower(strings.TrimSpace(text))
	normalized = strings.ReplaceAll(normalized, "’", "'")
	normalized = strings.TrimRight(normalized, ".。！!")
	switch normalized {
	case "copilot isn't available in your region", "copilot is not available in your region",
		"copilot 在你所在的区域不可用", "copilot 在你所在区域不可用", "copilot 在您所在的区域不可用":
		return true
	}
	return false
}
