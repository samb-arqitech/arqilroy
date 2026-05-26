package cursoragent

import "strings"

// DefaultModel is the default Cursor SDK model for Kilroy runs.
const DefaultModel = "composer-2.5"

// ToCursorModelID maps legacy provider CLI model names to Cursor SDK model IDs.
func ToCursorModelID(provider, modelID string) string {
	id := strings.TrimSpace(modelID)
	if id == "" {
		return DefaultModel
	}
	lower := strings.ToLower(id)
	if strings.HasPrefix(lower, "composer-") ||
		strings.HasPrefix(lower, "gpt-") ||
		strings.HasPrefix(lower, "claude-opus-4-") ||
		strings.HasPrefix(lower, "o3") ||
		strings.HasPrefix(lower, "o4") {
		return id
	}
	if strings.Contains(lower, "haiku") || strings.Contains(lower, "flash") || strings.Contains(lower, "mini") {
		return "composer-2-fast"
	}
	if strings.Contains(lower, "opus") || strings.Contains(lower, "sonnet") || strings.Contains(lower, "pro") {
		return DefaultModel
	}
	if strings.Contains(lower, "codex") || strings.Contains(lower, "gpt-5") {
		return DefaultModel
	}
	return DefaultModel
}
