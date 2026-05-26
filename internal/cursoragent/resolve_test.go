package cursoragent

import "testing"

func TestToCursorModelID(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"", DefaultModel},
		{"composer-2.5", "composer-2.5"},
		{"claude-sonnet-4-6", DefaultModel},
		{"claude-haiku-4-5", "composer-2-fast"},
		{"gemini-3-flash-preview", "composer-2-fast"},
		{"gpt-5.4-codex", "gpt-5.4-codex"},
	}
	for _, tc := range tests {
		if got := ToCursorModelID("anthropic", tc.in); got != tc.want {
			t.Fatalf("ToCursorModelID(%q) = %q want %q", tc.in, got, tc.want)
		}
	}
}
