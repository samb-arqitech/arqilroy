package modelmeta

import (
	"regexp"
	"strconv"
	"strings"

	"github.com/danshapiro/kilroy/internal/providerspec"
)

// DefaultOpenAIModel is the default OpenAI model used across the codebase.
// Change this single value to upgrade the default model everywhere.
const DefaultOpenAIModel = "gpt-5.4"

// DefaultAnthropicModel is the default model ID for anthropic-tagged graphs when
// routed through the Cursor SDK CLI bridge.
const DefaultAnthropicModel = "composer-2.5"

// DefaultCursorModel is the default Cursor SDK model ID.
const DefaultCursorModel = "composer-2.5"

// versionDotRe matches dots between digits in model version numbers
// (e.g. "4.5", "3.7") without touching other dots.
var versionDotRe = regexp.MustCompile(`(\d)\.(\d)`)

// ProviderRelativeModelID strips the "provider/" prefix from a model ID if
// present, returning the bare model name.
//
//	"anthropic/claude-opus-4.6" → "claude-opus-4.6"
//	"claude-opus-4.6"           → "claude-opus-4.6" (unchanged)
func ProviderRelativeModelID(provider, modelID string) string {
	p := strings.ToLower(strings.TrimSpace(provider))
	if p == "" {
		return modelID
	}
	prefix := p + "/"
	if strings.HasPrefix(strings.ToLower(modelID), prefix) {
		return modelID[len(prefix):]
	}
	return modelID
}

// NativeModelID converts an OpenRouter-format model ID to the native format
// required by the given provider's API or CLI.
//
//   - Strips "provider/" prefix if present
//   - For anthropic: converts digit.digit version separators to digit-digit
//     ("claude-opus-4.6" → "claude-opus-4-6")
//   - For all other providers: returns the bare model name unchanged
func NativeModelID(provider, modelID string) string {
	modelID = ProviderRelativeModelID(provider, modelID)
	if strings.ToLower(strings.TrimSpace(provider)) == "anthropic" {
		modelID = versionDotRe.ReplaceAllString(modelID, "${1}-${2}")
	}
	return modelID
}

func NormalizeProvider(p string) string {
	return providerspec.CanonicalProviderKey(p)
}

func ProviderFromModelID(id string) string {
	id = strings.TrimSpace(id)
	parts := strings.SplitN(id, "/", 2)
	if len(parts) < 2 {
		return ""
	}
	return NormalizeProvider(parts[0])
}

func ContainsFold(values []string, target string) bool {
	target = strings.ToLower(strings.TrimSpace(target))
	for _, v := range values {
		if strings.ToLower(strings.TrimSpace(v)) == target {
			return true
		}
	}
	return false
}

func ParseFloatStringPtr(v string) *float64 {
	v = strings.TrimSpace(v)
	if v == "" {
		return nil
	}
	f, err := strconv.ParseFloat(v, 64)
	if err != nil {
		return nil
	}
	return &f
}
