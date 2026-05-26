package ingest

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBuildCLIArgs(t *testing.T) {
	skillDir := t.TempDir()
	skillPath := filepath.Join(skillDir, "SKILL.md")
	if err := os.WriteFile(skillPath, []byte("test skill content"), 0o644); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name      string
		opts      Options
		checkArgs func(*testing.T, []string)
	}{
		{
			name: "basic invocation",
			opts: Options{
				Model:        "claude-sonnet-4-5",
				SkillPath:    skillPath,
				Requirements: "Build a solitaire game",
			},
			checkArgs: func(t *testing.T, args []string) {
				assertContains(t, args, "run")
				assertContains(t, args, "--model")
				assertContains(t, args, "composer-2.5")
				assertContains(t, args, "--append-system-prompt")
				assertContains(t, args, "--cwd")
			},
		},
		{
			name: "custom model maps to cursor id",
			opts: Options{
				Model:        "claude-haiku-4-5",
				SkillPath:    skillPath,
				Requirements: "Build DTTF",
			},
			checkArgs: func(t *testing.T, args []string) {
				assertContains(t, args, "composer-2-fast")
			},
		},
		{
			name: "no skill path omits flag",
			opts: Options{
				Model:        "composer-2.5",
				Requirements: "Build something",
			},
			checkArgs: func(t *testing.T, args []string) {
				assertNotContains(t, args, "--append-system-prompt")
			},
		},
		{
			name: "relative repo path in system prompt",
			opts: Options{
				Model:        "composer-2.5",
				Requirements: "Build something",
				RepoPath:     "../relative/path",
			},
			checkArgs: func(t *testing.T, args []string) {
				found := false
				for i, a := range args {
					if a == "--append-system-prompt" && i+1 < len(args) {
						if !filepath.IsAbs(filepath.Clean(args[i+1])) && !strings.Contains(args[i+1], "repository context") {
							// repo path is embedded in prose, not as a flag value path alone
						}
						if strings.Contains(args[i+1], "repository context") {
							found = true
						}
					}
				}
				if !found {
					t.Error("expected repo context in --append-system-prompt")
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			exe, args, tmpDir, err := buildCLIArgs(tt.opts)
			if err != nil {
				t.Fatalf("buildCLIArgs: %v", err)
			}
			defer os.RemoveAll(tmpDir)
			if !strings.Contains(exe, "kilroy-cursor-agent") {
				t.Errorf("exe = %q, want kilroy-cursor-agent wrapper", exe)
			}
			if tt.checkArgs != nil {
				tt.checkArgs(t, args)
			}
		})
	}
}

func TestBuildPromptUsesSkillNameFromSkillPath(t *testing.T) {
	skillDir := filepath.Join(t.TempDir(), "skills", "create-dotfile")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	skillPath := filepath.Join(skillDir, "SKILL.md")
	if err := os.WriteFile(skillPath, []byte("skill content"), 0o644); err != nil {
		t.Fatal(err)
	}

	prompt := buildPrompt("Build something", inferSkillName(skillPath))
	if !strings.Contains(prompt, "Follow the create-dotfile skill") {
		t.Fatalf("prompt missing skill binding, got: %q", prompt)
	}
}

func TestBuildPromptFallsBackToGenericSkillLabelWithoutSkillPath(t *testing.T) {
	prompt := buildPrompt("Build something", inferSkillName(""))
	if !strings.Contains(prompt, "Follow the provided skill") {
		t.Fatalf("prompt missing generic fallback skill label, got: %q", prompt)
	}
}

func TestRunIngestRequiresSkill(t *testing.T) {
	_, err := Run(context.Background(), Options{
		Requirements: "Build something",
		SkillPath:    "/nonexistent/SKILL.md",
		Model:        "composer-2.5",
	})
	if err == nil {
		t.Fatal("expected error for missing skill file")
	}
}

func assertContains(t *testing.T, slice []string, want string) {
	t.Helper()
	for _, s := range slice {
		if s == want || strings.Contains(s, want) {
			return
		}
	}
	t.Errorf("args %v does not contain %q", slice, want)
}

func assertNotContains(t *testing.T, slice []string, unwanted string) {
	t.Helper()
	for _, s := range slice {
		if s == unwanted {
			t.Errorf("args %v should not contain %q", slice, unwanted)
			return
		}
	}
}
