package corral

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// CLIProvider drives a headless coding-agent CLI. It is the shared mechanism the
// Codex and Claude Code provider packages build presets on (Antigravity uses its
// own ConPTY Driver instead). A turn is a single exec of the binary with the
// composed prompt; structured output is taken either from an --output file
// (Codex) or parsed from stdout. stdin is always /dev/null so the child never
// drains the parent's input.
type CLIProvider struct {
	ProviderName string   // "codex" | "claude" | ...
	Bin          string   // executable name on PATH
	BaseArgs     []string // fixed leading args (e.g. ["exec","--dangerously-bypass-approvals-and-sandbox"])
	DirFlag      string   // flag to set working dir (e.g. "-C"); "" => use cmd.Dir
	SchemaFlag   string   // flag taking a JSON-Schema file (e.g. "--output-schema"); "" => embed schema in prompt
	OutputFlag   string   // flag taking an output file (e.g. "-o"); "" => read stdout
	ModelFlag    string   // flag selecting the model (e.g. "--model"); "" => no flag emitted
	Model        string   // default model used when an Agent sets none (e.g. "claude-sonnet-4-6")
	PromptFlag   string   // flag carrying the prompt as its VALUE (e.g. "--single","--prompt"); "" => prompt is a trailing positional arg
	PromptStdin  bool     // CLI reads the prompt from stdin when it is NOT passed as an arg (verified: claude -p, codex exec). Required to deliver an oversized prompt; providers without it error loudly rather than emit a malformed call. See item #9.
	ImageFlag    string   // flag attaching a local image file for multimodal turns (e.g. codex "-i"), repeated per RunRequest.Images entry; "" => images ignored (the CLI reads them from Dir/prompt instead, e.g. Claude Code's Read tool).
}

func (p *CLIProvider) Name() string { return p.ProviderName }

// modelFor resolves the model for a turn: the Agent's explicit hint wins,
// otherwise the provider default (p.Model).
func (p *CLIProvider) modelFor(a Agent) string {
	if strings.TrimSpace(a.Model) != "" {
		return a.Model
	}
	return p.Model
}

// Open satisfies SessionOpener with a one-shot session: stateless CLIs (Codex
// exec) spawn one process per turn, so there is no warm process to reuse. The
// pool still gives a uniform interface. Providers with a real persistent mode
// (Antigravity's ConPTY Driver) return a genuinely warm Session instead.
func (p *CLIProvider) Open(_ context.Context, _ Agent) (Session, error) {
	return oneShotSession{p: p}, nil
}

// argv builds the command-line for a request.
func (p *CLIProvider) argv(req RunRequest, schemaPath, outPath, prompt string) (args []string, fromFile bool) {
	args = append(args, p.BaseArgs...)
	if model := p.modelFor(req.Agent); p.ModelFlag != "" && model != "" {
		args = append(args, p.ModelFlag, model)
	}
	if p.DirFlag != "" && req.Dir != "" {
		args = append(args, p.DirFlag, req.Dir)
	}
	if p.ImageFlag != "" {
		for _, img := range req.Images {
			args = append(args, p.ImageFlag, img)
		}
	}
	if schemaPath != "" && p.SchemaFlag != "" {
		args = append(args, p.SchemaFlag, schemaPath)
		if p.OutputFlag != "" {
			args = append(args, p.OutputFlag, outPath)
			fromFile = true
		}
	}
	if prompt != "" {
		if p.PromptFlag != "" {
			args = append(args, p.PromptFlag, prompt) // prompt is this flag's value (grok --single, kimi --prompt)
		} else {
			args = append(args, prompt) // trailing positional (claude -p, codex exec)
		}
	}
	return args, fromFile
}

// maxArgPrompt caps how large a prompt may be before it is fed on stdin instead
// of as a command-line argument. Windows limits a whole command line to ~32 KB
// (CreateProcess), and the schema-embedded prompt for a big concept blows past
// that, failing with "The filename or extension is too long". Both `claude -p`
// and `codex exec` read the prompt from stdin, so large turns route there.
const maxArgPrompt = 8000

// promptDelivery decides how the prompt reaches the child process: as an argv
// value (prompts within the command-line limit) or on stdin (oversized prompts,
// only for providers whose CLI reads it there). An oversized prompt for a
// provider that cannot take stdin is a loud error — NOT a silently dropped flag,
// which would otherwise produce a malformed invocation (e.g. grok --single with
// no value). See item #9. A future improvement is a per-provider prompt-file
// flag (grok exposes `--prompt-file`), which sidesteps both the arg limit and
// stdin entirely.
func (p *CLIProvider) promptDelivery(prompt string) (argPrompt string, useStdin bool, err error) {
	if len(prompt) <= maxArgPrompt {
		return prompt, false, nil
	}
	if !p.PromptStdin {
		return "", false, fmt.Errorf("%s: prompt is %d bytes, over the %d-byte command-line limit, and this provider does not read the prompt from stdin (item #9)", p.ProviderName, len(prompt), maxArgPrompt)
	}
	return "", true, nil
}

// Run executes one agent turn and returns its text (or raw JSON when a schema
// was requested).
func (p *CLIProvider) Run(ctx context.Context, req RunRequest) (RunResult, error) {
	schema := req.EffectiveSchema()
	nativeSchema := schema != "" && p.SchemaFlag != ""
	prompt := req.ComposePrompt(!nativeSchema) // embed schema in prompt only when not native

	var schemaPath, outPath string
	var cleanup []string
	defer func() {
		for _, f := range cleanup {
			_ = os.Remove(f)
		}
	}()
	if nativeSchema {
		sf, err := os.CreateTemp("", "agent-schema-*.json")
		if err != nil {
			return RunResult{}, fmt.Errorf("%s: schema temp: %w", p.ProviderName, err)
		}
		if _, err := sf.WriteString(schema); err != nil {
			_ = sf.Close()
			return RunResult{}, fmt.Errorf("%s: write schema: %w", p.ProviderName, err)
		}
		_ = sf.Close()
		schemaPath = sf.Name()
		cleanup = append(cleanup, schemaPath)
		if p.OutputFlag != "" {
			of, err := os.CreateTemp("", "agent-out-*.json")
			if err != nil {
				return RunResult{}, fmt.Errorf("%s: out temp: %w", p.ProviderName, err)
			}
			_ = of.Close()
			outPath = of.Name()
			cleanup = append(cleanup, outPath)
		}
	}

	// Feed an oversized prompt on stdin instead of as an argv entry to stay under
	// the OS command-line length limit (Windows ~32 KB) — but only for providers
	// whose CLI reads stdin; others error rather than emit a malformed call.
	argPrompt, useStdin, derr := p.promptDelivery(prompt)
	if derr != nil {
		return RunResult{}, derr
	}
	args, fromFile := p.argv(req, schemaPath, outPath, argPrompt)
	cmd := exec.CommandContext(ctx, p.Bin, args...)
	if p.DirFlag == "" && req.Dir != "" {
		cmd.Dir = req.Dir
	}
	if useStdin {
		cmd.Stdin = strings.NewReader(prompt)
	} else {
		devnull, err := os.Open(os.DevNull)
		if err != nil {
			return RunResult{}, fmt.Errorf("%s: open devnull: %w", p.ProviderName, err)
		}
		defer func() { _ = devnull.Close() }()
		cmd.Stdin = devnull
	}

	out, err := cmd.Output()
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) && len(ee.Stderr) > 0 {
			return RunResult{}, fmt.Errorf("%s run: %w: %s", p.ProviderName, err, strings.TrimSpace(string(ee.Stderr)))
		}
		return RunResult{}, fmt.Errorf("%s run: %w", p.ProviderName, err)
	}

	text := strings.TrimSpace(string(out))
	if fromFile {
		b, rerr := os.ReadFile(outPath)
		if rerr != nil {
			return RunResult{}, fmt.Errorf("%s: read output: %w", p.ProviderName, rerr)
		}
		text = strings.TrimSpace(string(b))
	}
	return RunResult{Text: text, Provider: p.ProviderName}, nil
}
