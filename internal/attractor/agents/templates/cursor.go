// Cursor SDK invocation template (kilroy-cursor-agent bridge).
package templates

import (
	"os"
	"time"

	"github.com/danshapiro/kilroy/internal/cursoragent"
)

// Cursor returns an invocation template for the @cursor/sdk local agent bridge.
func Cursor() Template {
	return Template{
		Name:       "cursor",
		Binary:     cursoragent.ResolveExecutable(),
		LogLocator: nil,
		BuildArgs: func(prompt, workDir, model string) []string {
			args := []string{
				"run",
				"--cwd", workDir,
				"--model", cursoragent.ToCursorModelID("", model),
				"--stream-json",
			}
			_ = prompt // prompt is passed on stdin by the session manager
			return args
		},
		BuildEnv: func() map[string]string {
			env := map[string]string{}
			if key := os.Getenv("CURSOR_API_KEY"); key != "" {
				env["CURSOR_API_KEY"] = key
			}
			return env
		},
		PromptFileFlag:   "--prompt-file",
		StructuredOutput: true,
		PromptPrefix:     "",
		BusyIndicators:   []string{"[tool]", "[status]"},
		ProcessNames:     []string{"node", "kilroy-cursor-agent"},
		ExitsOnComplete:  true,
		StartupDialogs:   nil,
		StartupTimeout:   30 * time.Second,
	}
}
