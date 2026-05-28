package ingest

import (
	"bytes"
	"context"
	_ "embed"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"text/template"

	"github.com/danshapiro/kilroy/internal/attractor/engine"
	"github.com/danshapiro/kilroy/internal/cursoragent"
)

//go:embed ingest_prompt.tmpl
var ingestPromptTmpl string

var ingestPrompt = template.Must(template.New("ingest").Parse(ingestPromptTmpl))

const outputFilename = "pipeline.dot"

// Options configures an ingestion run.
type Options struct {
	Requirements string // The English requirements text.
	SkillPath    string // Path to the SKILL.md file.
	Model        string // LLM model ID.
	RepoPath     string // Repository root (add-dir / context for the agent).
	Validate     bool   // Whether to validate the .dot output.
	MaxTurns     int    // Reserved; Cursor SDK manages turn budget internally.
}

// Result contains the output of an ingestion run.
type Result struct {
	DotContent string   // The extracted .dot file content.
	Warnings   []string // Any validation warnings.
}

// buildPrompt renders the ingest prompt template with the given requirements.
func buildPrompt(requirements, skillName string) string {
	var buf bytes.Buffer
	data := struct {
		Requirements string
		SkillName    string
	}{
		Requirements: requirements,
		SkillName:    skillName,
	}
	if err := ingestPrompt.Execute(&buf, data); err != nil {
		// Embedded template execution should not fail; keep ingest usable with
		// an explicit fallback prompt.
		return fmt.Sprintf(
			"Follow the %s skill in your system prompt exactly.\n\nWrite the final .dot pipeline to %s in your working directory.\nDo NOT write any other files. You must ONLY execute the skill, and you must NOT implement software directly.\n\nREQUIREMENTS:\n%s",
			skillName,
			outputFilename,
			requirements,
		)
	}
	return buf.String()
}

func inferSkillName(skillPath string) string {
	const fallback = "provided"
	if strings.TrimSpace(skillPath) == "" {
		return fallback
	}
	base := filepath.Base(skillPath)
	if strings.EqualFold(base, "SKILL.md") {
		parent := filepath.Base(filepath.Dir(skillPath))
		if parent != "" && parent != "." && parent != string(filepath.Separator) {
			return parent
		}
	}
	return fallback
}

func buildCLIArgs(opts Options) (string, []string, string, error) {
	exe := envOr(cursoragent.EnvPath, cursoragent.ResolveExecutable())

	model := cursoragent.ToCursorModelID("anthropic", opts.Model)
	args := []string{
		"run",
		"--model", model,
		"--interactive",
	}

	// Create a temp working directory so the agent writes pipeline.dot here.
	tmpDir, err := os.MkdirTemp("", "kilroy-ingest-*")
	if err != nil {
		return "", nil, "", fmt.Errorf("creating temp directory: %w", err)
	}
	args = append(args, "--cwd", tmpDir)

	var systemParts []string
	if opts.SkillPath != "" {
		skillContent, err := os.ReadFile(opts.SkillPath)
		if err != nil {
			return "", nil, "", fmt.Errorf("reading skill file: %w", err)
		}
		if len(skillContent) > 0 {
			systemParts = append(systemParts, string(skillContent))
		}
	}
	if opts.RepoPath != "" {
		absRepo, err := filepath.Abs(opts.RepoPath)
		if err != nil {
			return "", nil, "", fmt.Errorf("resolving repo path: %w", err)
		}
		systemParts = append(systemParts, fmt.Sprintf("Additional repository context is available at: %s", absRepo))
	}
	if len(systemParts) > 0 {
		args = append(args, "--append-system-prompt", strings.Join(systemParts, "\n\n"))
	}

	_ = opts.MaxTurns

	return exe, args, tmpDir, nil
}

// Run executes the ingestion: invokes the Cursor SDK agent bridge with the skill
// and requirements. The agent writes pipeline.dot in its working directory,
// which is read back after the session ends.
func Run(ctx context.Context, opts Options) (*Result, error) {
	// Verify skill file exists.
	if _, err := os.Stat(opts.SkillPath); err != nil {
		return nil, fmt.Errorf("skill file not found: %s: %w", opts.SkillPath, err)
	}

	exe, args, tmpDir, err := buildCLIArgs(opts)
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tmpDir)

	prompt := buildPrompt(opts.Requirements, inferSkillName(opts.SkillPath))
	cmd := exec.CommandContext(ctx, exe, args...)
	cmd.Dir = tmpDir
	cmd.Stdin = strings.NewReader(prompt)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err = cmd.Run(); err != nil {
		return nil, fmt.Errorf("cursor agent exited with error: %v", err)
	}

	dotPath := filepath.Join(tmpDir, outputFilename)
	dotBytes, err := os.ReadFile(dotPath)
	if err != nil {
		return nil, fmt.Errorf("cursor agent did not write %s: %w", outputFilename, err)
	}

	dotContent := strings.TrimSpace(string(dotBytes))
	if dotContent == "" {
		return nil, fmt.Errorf("%s is empty", outputFilename)
	}

	result := &Result{
		DotContent: dotContent,
	}

	// Optionally validate.
	if opts.Validate {
		_, diags, err := engine.Prepare([]byte(dotContent))
		if err != nil {
			return result, fmt.Errorf("generated .dot failed validation: %w", err)
		}
		for _, d := range diags {
			result.Warnings = append(result.Warnings, fmt.Sprintf("%s: %s (%s)", d.Severity, d.Message, d.Rule))
		}
	}

	return result, nil
}

func envOr(key, def string) string {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return def
	}
	return v
}
