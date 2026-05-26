package review

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strings"
	"sync"

	"github.com/danshapiro/kilroy/internal/attractor/engine"
	"github.com/danshapiro/kilroy/internal/attractor/model"
	"github.com/danshapiro/kilroy/internal/attractor/validate"
	"github.com/danshapiro/kilroy/internal/cursoragent"
)

// Options configures a review run.
type Options struct {
	GraphPath string
	DotSource string
	RepoPath  string
	MaxTurns  int // per expert; default 3
}

// CycleEdge describes a back edge (cycle) detected in the graph.
type CycleEdge struct {
	From        string
	To          string // backward target (loop entry point)
	Condition   string
	LoopRestart bool
	LoopBody    []string // node IDs on the cycle path (inclusive of To)
}

// LoopAnalysis holds the per-loop expert verdict.
type LoopAnalysis struct {
	EntryNode   string   `json:"entry_node"`
	BackEdgeTo  string   `json:"back_edge_to"`
	Condition   string   `json:"condition"`
	Verdict     string   `json:"verdict"` // "ok" | "warning" | "error"
	Score       int      `json:"score"`   // 0-100
	Issues      []string `json:"issues"`
	Suggestions []string `json:"suggestions"`
}

// ReviewReport aggregates all loop analyses for a graph.
type ReviewReport struct {
	File             string         `json:"file"`
	LoopCount        int            `json:"loop_count"`
	Loops            []LoopAnalysis `json:"loops"`
	ValidateErrors   int            `json:"validate_errors"`
	ValidateWarnings int            `json:"validate_warnings"`
	OverallScore     int            `json:"overall_score"`
	Summary          string         `json:"summary"`
}

// Markdown returns a human-readable markdown representation of the report.
func (r *ReviewReport) Markdown() string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("# Loop Semantics Review: %s\n\n", r.File))
	sb.WriteString(fmt.Sprintf("**Overall Score:** %d/100  **Loops:** %d  **Validate Errors:** %d  **Warnings:** %d\n\n",
		r.OverallScore, r.LoopCount, r.ValidateErrors, r.ValidateWarnings))
	sb.WriteString(fmt.Sprintf("**Summary:** %s\n\n", r.Summary))
	for i, loop := range r.Loops {
		sb.WriteString(fmt.Sprintf("## Loop %d: %s → %s\n", i+1, loop.EntryNode, loop.BackEdgeTo))
		sb.WriteString(fmt.Sprintf("- **Verdict:** %s  **Score:** %d/100\n", loop.Verdict, loop.Score))
		if loop.Condition != "" {
			sb.WriteString(fmt.Sprintf("- **Back edge condition:** `%s`\n", loop.Condition))
		}
		if len(loop.Issues) > 0 {
			sb.WriteString("- **Issues:**\n")
			for _, issue := range loop.Issues {
				sb.WriteString(fmt.Sprintf("  - %s\n", issue))
			}
		}
		if len(loop.Suggestions) > 0 {
			sb.WriteString("- **Suggestions:**\n")
			for _, sug := range loop.Suggestions {
				sb.WriteString(fmt.Sprintf("  - %s\n", sug))
			}
		}
		sb.WriteString("\n")
	}
	return sb.String()
}

// Run parses the graph, detects cycles, and dispatches per-loop expert analyses.
func Run(ctx context.Context, opts Options) (*ReviewReport, error) {
	dotSource := []byte(opts.DotSource)
	if opts.GraphPath != "" && len(dotSource) == 0 {
		var err error
		dotSource, err = os.ReadFile(opts.GraphPath)
		if err != nil {
			return nil, fmt.Errorf("reading graph: %w", err)
		}
	}

	g, diags, parseErr := engine.PrepareWithOptions(dotSource, engine.PrepareOptions{})
	if parseErr != nil && g == nil {
		return &ReviewReport{
			File:    opts.GraphPath,
			Summary: fmt.Sprintf("Parse error: %v", parseErr),
		}, nil
	}

	var errCount, warnCount int
	for _, d := range diags {
		switch d.Severity {
		case validate.SeverityError:
			errCount++
		case validate.SeverityWarning:
			warnCount++
		}
	}

	cycles := detectCycles(g)

	report := &ReviewReport{
		File:             opts.GraphPath,
		LoopCount:        len(cycles),
		ValidateErrors:   errCount,
		ValidateWarnings: warnCount,
	}

	if len(cycles) == 0 {
		score := 100 - 10*errCount - 3*warnCount
		if score < 0 {
			score = 0
		}
		report.OverallScore = score
		report.Summary = fmt.Sprintf("No loops detected. Validate: %d errors, %d warnings.", errCount, warnCount)
		return report, nil
	}

	// Spawn parallel expert goroutines — one per cycle.
	analyses := make([]LoopAnalysis, len(cycles))
	var wg sync.WaitGroup
	for i, cycle := range cycles {
		i, cycle := i, cycle
		wg.Add(1)
		go func() {
			defer wg.Done()
			analyses[i] = analyzeLoop(ctx, g, cycle, diags, opts)
		}()
	}
	wg.Wait()
	report.Loops = analyses

	// Compute overall score: mean loop score penalized by validate issues.
	totalScore := 0
	for _, a := range analyses {
		totalScore += a.Score
	}
	meanScore := totalScore / len(analyses)
	score := meanScore - 10*errCount - 3*warnCount
	if score < 0 {
		score = 0
	}
	report.OverallScore = score

	// Build summary.
	var verdicts []string
	for _, a := range analyses {
		verdicts = append(verdicts, fmt.Sprintf("%s→%s:%s", a.EntryNode, a.BackEdgeTo, a.Verdict))
	}
	report.Summary = fmt.Sprintf("%d loop(s): %s. Validate: %d errors, %d warnings.",
		len(cycles), strings.Join(verdicts, "; "), errCount, warnCount)

	return report, nil
}

// detectCycles finds all back edges in the graph via DFS and returns CycleEdge descriptors.
func detectCycles(g *model.Graph) []CycleEdge {
	if g == nil {
		return nil
	}
	visited := map[string]bool{}
	inStack := map[string]bool{}
	stack := []string{}
	var cycles []CycleEdge

	var dfs func(nodeID string)
	dfs = func(nodeID string) {
		visited[nodeID] = true
		inStack[nodeID] = true
		stack = append(stack, nodeID)

		for _, edge := range g.Outgoing(nodeID) {
			target := edge.To
			if !visited[target] {
				dfs(target)
			} else if inStack[target] {
				// Back edge: found a cycle back to target.
				startIdx := -1
				for i, id := range stack {
					if id == target {
						startIdx = i
						break
					}
				}
				loopBody := make([]string, len(stack)-startIdx)
				copy(loopBody, stack[startIdx:])
				cycles = append(cycles, CycleEdge{
					From:        nodeID,
					To:          target,
					Condition:   edge.Condition(),
					LoopRestart: edge.Attr("loop_restart", "") == "true",
					LoopBody:    loopBody,
				})
			}
		}

		stack = stack[:len(stack)-1]
		inStack[nodeID] = false
	}

	// Start from the start node (Mdiamond shape or id="start").
	if startNodeID := findStartNode(g); startNodeID != "" {
		dfs(startNodeID)
	}

	// Visit any remaining unvisited nodes (disconnected subgraphs).
	ids := make([]string, 0, len(g.Nodes))
	for id := range g.Nodes {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		if !visited[id] {
			dfs(id)
		}
	}

	return cycles
}

func findStartNode(g *model.Graph) string {
	for _, n := range g.Nodes {
		if strings.ToLower(n.Shape()) == "mdiamond" {
			return n.ID
		}
	}
	if _, ok := g.Nodes["start"]; ok {
		return "start"
	}
	return ""
}

// analyzeLoop calls the Cursor SDK bridge to evaluate one cycle.
func analyzeLoop(ctx context.Context, g *model.Graph, cycle CycleEdge, diags []validate.Diagnostic, opts Options) LoopAnalysis {
	analysis := LoopAnalysis{
		EntryNode:  cycle.To,
		BackEdgeTo: cycle.To,
		Condition:  cycle.Condition,
	}

	maxTurns := opts.MaxTurns
	if maxTurns <= 0 {
		maxTurns = 3
	}

	prompt := buildLoopPrompt(g, cycle, diags)
	_ = maxTurns
	exe := cursorAgentExe()
	workDir := "."
	if opts.RepoPath != "" {
		workDir = opts.RepoPath
	}
	cmd := exec.CommandContext(ctx, exe,
		"run",
		"--cwd", workDir,
		"--model", cursoragent.DefaultModel,
		"--output-format", "json",
	)
	cmd.Stdin = strings.NewReader(prompt)
	cmd.Env = reviewFilteredEnv()

	out, err := cmd.Output()
	if err != nil {
		analysis.Verdict = "error"
		analysis.Score = 0
		analysis.Issues = []string{fmt.Sprintf("cursor agent invocation failed: %v", err)}
		return analysis
	}

	// Parse the JSON envelope: {"result": "...", "is_error": bool, ...}
	var envelope struct {
		Result  string `json:"result"`
		IsError bool   `json:"is_error"`
	}
	if err := json.Unmarshal(out, &envelope); err != nil {
		analysis.Verdict = "warning"
		analysis.Score = 50
		analysis.Issues = []string{fmt.Sprintf("could not parse cursor agent output envelope: %v", err)}
		return analysis
	}
	if envelope.IsError {
		analysis.Verdict = "error"
		analysis.Score = 0
		analysis.Issues = []string{fmt.Sprintf("cursor agent returned an error: %s", envelope.Result)}
		return analysis
	}

	// Extract JSON from the result text (the model should emit a JSON block).
	var loopJSON struct {
		Verdict     string   `json:"verdict"`
		Score       int      `json:"score"`
		Issues      []string `json:"issues"`
		Suggestions []string `json:"suggestions"`
	}
	result := envelope.Result
	if startBrace := strings.Index(result, "{"); startBrace >= 0 {
		if err := json.Unmarshal([]byte(result[startBrace:]), &loopJSON); err == nil {
			analysis.Verdict = loopJSON.Verdict
			analysis.Score = loopJSON.Score
			analysis.Issues = loopJSON.Issues
			analysis.Suggestions = loopJSON.Suggestions
			return analysis
		}
	}

	// Fallback: couldn't extract structured data.
	analysis.Verdict = "warning"
	analysis.Score = 50
	analysis.Issues = []string{"could not extract structured JSON from expert analysis"}
	return analysis
}

// buildLoopPrompt constructs the expert prompt for a single loop.
func buildLoopPrompt(g *model.Graph, cycle CycleEdge, diags []validate.Diagnostic) string {
	var sb strings.Builder
	sb.WriteString("You are a loop-semantics expert for Kilroy Attractor DOT graphs.\n")
	sb.WriteString("Analyze the following loop and score it on 4 dimensions (25pts each).\n\n")

	sb.WriteString(fmt.Sprintf("## Loop: %s → (back to) %s\n", cycle.From, cycle.To))
	sb.WriteString(fmt.Sprintf("- Back edge condition: `%s`\n", cycle.Condition))
	sb.WriteString(fmt.Sprintf("- loop_restart: %v\n\n", cycle.LoopRestart))

	sb.WriteString("## Loop body nodes\n")
	for _, nodeID := range cycle.LoopBody {
		n := g.Nodes[nodeID]
		if n == nil {
			continue
		}
		prompt := n.Prompt()
		if len(prompt) > 200 {
			prompt = prompt[:200] + "..."
		}
		sb.WriteString(fmt.Sprintf("- **%s** shape=%s auto_status=%s max_retries=%s\n",
			nodeID, n.Shape(),
			n.Attr("auto_status", "false"),
			n.Attr("max_retries", "-")))
		if prompt != "" {
			sb.WriteString(fmt.Sprintf("  prompt_excerpt: %s\n", prompt))
		}
	}

	sb.WriteString("\n## Outgoing edges from loop body\n")
	for _, nodeID := range cycle.LoopBody {
		for _, e := range g.Outgoing(nodeID) {
			cond := e.Condition()
			lr := e.Attr("loop_restart", "")
			desc := fmt.Sprintf("  %s → %s", nodeID, e.To)
			if cond != "" {
				desc += fmt.Sprintf(" [condition=%q]", cond)
			}
			if lr == "true" {
				desc += " [loop_restart=true]"
			}
			sb.WriteString(desc + "\n")
		}
	}

	// Include validator diagnostics touching loop nodes.
	bodySet := map[string]bool{}
	for _, id := range cycle.LoopBody {
		bodySet[id] = true
	}
	var relevant []validate.Diagnostic
	for _, d := range diags {
		if bodySet[d.NodeID] || bodySet[d.EdgeFrom] || bodySet[d.EdgeTo] {
			relevant = append(relevant, d)
		}
	}
	if len(relevant) > 0 {
		sb.WriteString("\n## Validator diagnostics touching loop nodes\n")
		for _, d := range relevant {
			sb.WriteString(fmt.Sprintf("- %s (%s): %s\n", d.Severity, d.Rule, d.Message))
		}
	}

	sb.WriteString(`
## Scoring rubric (25pts each dimension)

1. **Termination** (25pts)
   - 25: loop_restart=true on transient_infra condition, OR outcome=more_work with outcome=all_done exit AND stall guard
   - 12: outcome-keyed back edge but missing stall guard (pass counter or max_retries)
   - 0: unconditional back edge (infinite loop)

2. **Work preservation** (25pts)
   - 25: body nodes have auto_status=true OR prompt says "skip if exists"
   - 12: some nodes skip, others unconditionally overwrite
   - 0: unconditional rewrites on every pass

3. **Failure escalation** (25pts)
   - 25: loop has outcome=fail / failure_class!=transient_infra path routing to postmortem
   - 12: escalation exists but is hard to trigger (buried condition)
   - 0: no escalation path to postmortem

4. **Re-entry appropriateness** (25pts)
   - 25: back edge targets a fan-out (shape=component) or planner node
   - 12: back edge targets a mid-pipeline box node
   - 0: back edge targets the very first node (full restart), discarding all work

## Required output format (JSON only, no other text)
{"verdict":"ok|warning|error","score":<0-100>,"issues":[<list of specific problems>],"suggestions":[<list of actionable fixes>]}
`)
	return sb.String()
}

// reviewFilteredEnv returns os.Environ() with Claude Code session variables stripped
// to prevent nested agent detection issues.
func reviewFilteredEnv() []string {
	stripKeys := map[string]bool{
		"CLAUDECODE":           true,
		"CLAUDE_CODE_SSE_PORT": true,
	}
	out := make([]string, 0, len(os.Environ()))
	for _, kv := range os.Environ() {
		k, _, ok := strings.Cut(kv, "=")
		if !ok {
			continue
		}
		if stripKeys[k] {
			continue
		}
		out = append(out, kv)
	}
	return out
}

func cursorAgentExe() string {
	if v := strings.TrimSpace(os.Getenv("KILROY_CURSOR_AGENT_PATH")); v != "" {
		return v
	}
	if v := strings.TrimSpace(os.Getenv(cursoragent.EnvPath)); v != "" {
		return v
	}
	return cursoragent.ResolveExecutable()
}
