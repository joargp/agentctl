package session

import "strings"

// NormalizeModelName canonicalizes model names for display/filtering/grouping.
// Historical logs sometimes store bare Gemini names (e.g. "gemini-3.1-pro-preview")
// even when the original run used provider-qualified syntax.
func NormalizeModelName(model string) string {
	if model == "" || strings.Contains(model, "/") {
		return model
	}
	if strings.HasPrefix(model, "gemini-") {
		return "google/" + model
	}
	return model
}
