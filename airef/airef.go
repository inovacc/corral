// Package airef is a reference catalog + integration-snippet generator for the
// AI providers corral talks to (Anthropic, OpenAI, Google Gemini, OpenRouter).
//
// It carries two things:
//
//   - a models catalog with each provider's current model IDs and the
//     reasoning-"effort" control they expose (Anthropic output_config.effort,
//     OpenAI reasoning_effort, Gemini thinkingConfig, OpenRouter reasoning), and
//   - a snippet generator that emits ready-to-copy integration code in curl, Go,
//     Python, TypeScript/JavaScript, Java, and Rust — using each vendor's
//     OFFICIAL SDK where one exists, and the REST wire format otherwise.
//
// The generated snippets are reference text: corral does not import the vendor
// SDKs, so this package (and corral) stay dependency-light. Keys are read from
// the provider's environment variable in every snippet — never inlined.
package airef

import (
	"fmt"
	"io"
	"sort"
	"strings"
)

// Model is one model ID plus whether it exposes a reasoning-effort control.
type Model struct {
	ID        string
	Reasoning bool
	Note      string
}

// Effort describes a provider's reasoning-effort / thinking-budget control.
type Effort struct {
	Param   string   // request field, e.g. "output_config.effort"
	Levels  []string // accepted values (or a range description)
	Summary string
}

// Provider is one AI backend.
type Provider struct {
	Name         string // catalog key: anthropic | openai | gemini | openrouter
	Title        string
	KeyEnv       string // env var the SDKs/snippets read the key from
	DocURL       string
	DefaultModel string
	Models       []Model
	Effort       Effort
	family       string // wire family: anthropic | openai | gemini
}

var catalog = map[string]Provider{
	"anthropic": {
		Name: "anthropic", Title: "Anthropic (Claude)", KeyEnv: "ANTHROPIC_API_KEY",
		DocURL: "https://platform.claude.com/docs", DefaultModel: "claude-opus-4-8", family: "anthropic",
		Models: []Model{
			{"claude-opus-4-8", true, "most capable Opus"},
			{"claude-sonnet-4-6", true, "balanced"},
			{"claude-haiku-4-5", true, "fast/cheap"},
			{"claude-fable-5", true, "most capable; thinking always on"},
		},
		Effort: Effort{
			Param:  "output_config.effort",
			Levels: []string{"low", "medium", "high", "xhigh", "max"},
			Summary: "GA effort control (no beta header); pair with adaptive thinking " +
				"(thinking:{type:\"adaptive\"}). budget_tokens is removed on 4.7+/Fable/Sonnet-5.",
		},
	},
	"openai": {
		Name: "openai", Title: "OpenAI", KeyEnv: "OPENAI_API_KEY",
		DocURL: "https://platform.openai.com/docs", DefaultModel: "gpt-4o-mini", family: "openai",
		Models: []Model{
			{"gpt-4o", false, "chat"},
			{"gpt-4o-mini", false, "fast chat"},
			{"gpt-5", true, "reasoning (reasoning_effort)"},
			{"o4-mini", true, "reasoning, small"},
		},
		Effort: Effort{
			Param:   "reasoning_effort",
			Levels:  []string{"minimal", "low", "medium", "high"},
			Summary: "reasoning_effort applies to reasoning models (o-series / gpt-5); ignored by gpt-4o chat models.",
		},
	},
	"gemini": {
		Name: "gemini", Title: "Google Gemini", KeyEnv: "GEMINI_API_KEY",
		DocURL: "https://ai.google.dev/gemini-api/docs", DefaultModel: "gemini-2.5-flash", family: "gemini",
		Models: []Model{
			{"gemini-2.5-pro", true, "most capable; thinking"},
			{"gemini-2.5-flash", true, "fast; thinking (disable with budget 0)"},
			{"gemini-2.5-flash-lite", true, "cheapest"},
		},
		Effort: Effort{
			Param:   "generationConfig.thinkingConfig.thinkingBudget",
			Levels:  []string{"0 (off)", "-1 (dynamic)", "N (token budget)"},
			Summary: "2.5 models are 'thinking' models; set thinkingBudget 0 to disable, -1 for dynamic, or an int budget.",
		},
	},
	"openrouter": {
		Name: "openrouter", Title: "OpenRouter (OpenAI-compatible router)", KeyEnv: "OPENROUTER_API_KEY",
		DocURL: "https://openrouter.ai/docs", DefaultModel: "anthropic/claude-opus-4-8", family: "openai",
		Models: []Model{
			{"anthropic/claude-opus-4-8", true, "via router"},
			{"openai/gpt-4o-mini", false, "via router"},
			{"google/gemini-2.5-flash", true, "via router"},
		},
		Effort: Effort{
			Param:   "reasoning.effort",
			Levels:  []string{"low", "medium", "high"},
			Summary: "OpenAI-compatible /chat/completions at https://openrouter.ai/api/v1; reasoning passes through per model.",
		},
	},
}

var order = []string{"anthropic", "openai", "gemini", "openrouter"}

// Providers returns the catalog in stable order.
func Providers() []Provider {
	out := make([]Provider, 0, len(order))
	for _, n := range order {
		out = append(out, catalog[n])
	}
	return out
}

// Lookup returns the provider by name (case-insensitive).
func Lookup(name string) (Provider, bool) {
	p, ok := catalog[strings.ToLower(strings.TrimSpace(name))]
	return p, ok
}

// Languages the snippet generator can emit.
var Languages = []string{"curl", "go", "python", "typescript", "javascript", "java", "rust"}

func validLang(l string) bool {
	for _, x := range Languages {
		if x == l {
			return true
		}
	}
	return false
}

// Opts tune a generated snippet.
type Opts struct {
	Model  string // empty => provider default
	Effort string // empty => omit the effort field
	Stream bool
}

// Snippet renders integration code for provider p in language lang.
func Snippet(p Provider, lang string, o Opts) (string, error) {
	lang = strings.ToLower(strings.TrimSpace(lang))
	if lang == "js" {
		lang = "javascript"
	}
	if lang == "ts" {
		lang = "typescript"
	}
	if !validLang(lang) {
		return "", fmt.Errorf("unknown language %q (want %s)", lang, strings.Join(Languages, ", "))
	}
	model := o.Model
	if model == "" {
		model = p.DefaultModel
	}
	r := replacer{model: model, effort: o.Effort, stream: o.Stream, key: p.KeyEnv}

	var tmpl string
	switch p.family {
	case "anthropic":
		tmpl = anthropicSnippets[lang]
	case "gemini":
		tmpl = geminiSnippets[lang]
	default: // openai + openrouter
		r.baseURL = "https://api.openai.com/v1"
		if p.Name == "openrouter" {
			r.baseURL = "https://openrouter.ai/api/v1"
		}
		tmpl = openaiSnippets[lang]
	}
	if tmpl == "" {
		return restFallback(p, lang, model, o), nil
	}
	out := r.apply(tmpl)
	if note := notes(p, o); note != "" {
		out += "\n" + note
	}
	return out, nil
}

// notes appends effort + streaming guidance appropriate to the provider, so the
// template body stays valid regardless of the flags.
func notes(p Provider, o Opts) string {
	var b strings.Builder
	if o.Effort != "" {
		switch p.family {
		case "anthropic":
			fmt.Fprintf(&b, "// effort: add output_config:{effort:%q} (GA) + thinking:{type:\"adaptive\"}.\n", o.Effort)
		case "gemini":
			fmt.Fprintf(&b, "// effort: set generationConfig.thinkingConfig.thinkingBudget (%s).\n", o.Effort)
		default:
			if p.Name == "openrouter" {
				fmt.Fprintf(&b, "// effort: add reasoning:{effort:%q} to the request body.\n", o.Effort)
			} else {
				fmt.Fprintf(&b, "// effort: add reasoning_effort:%q (reasoning models only).\n", o.Effort)
			}
		}
	}
	if o.Stream {
		fmt.Fprintf(&b, "// streaming: swap the call for the SDK's stream helper "+
			"(Anthropic Messages.NewStreaming / OpenAI .stream / Gemini GenerateContentStream) "+
			"and accumulate deltas.\n")
	}
	return b.String()
}

type replacer struct {
	model, effort, key, baseURL string
	stream                      bool
}

func (r replacer) apply(t string) string {
	rep := strings.NewReplacer(
		"{{MODEL}}", r.model,
		"{{KEYENV}}", r.key,
		"{{BASEURL}}", r.baseURL,
		"{{STREAM}}", boolStr(r.stream),
		"{{EFFORT}}", r.effort,
	)
	return strings.TrimRight(rep.Replace(t), "\n") + "\n"
}

func boolStr(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

// restFallback returns a language-appropriate REST pointer when no official-SDK
// template is bundled for that provider×language cell.
func restFallback(p Provider, lang, model string, o Opts) string {
	var b strings.Builder
	fmt.Fprintf(&b, "// %s — %s: no bundled official-SDK template for %q yet.\n", p.Title, model, lang)
	fmt.Fprintf(&b, "// Use the REST wire format (see the curl snippet: `corral snippet gen -p %s -l curl`)\n", p.Name)
	fmt.Fprintf(&b, "// or the official SDK: %s\n", p.DocURL)
	if o.Effort != "" {
		fmt.Fprintf(&b, "// effort: set %s = %s\n", p.Effort.Param, o.Effort)
	}
	return b.String()
}

// RenderCatalog writes the curated catalog (models + effort control) for the
// given providers to w. Callers pass Providers() (or a filtered subset).
func RenderCatalog(w io.Writer, providers []Provider) {
	for _, p := range providers {
		fmt.Fprintf(w, "%s  (%s)\n", p.Title, p.Name)
		fmt.Fprintf(w, "  key env : %s\n", p.KeyEnv)
		fmt.Fprintf(w, "  default : %s\n", p.DefaultModel)
		ids := make([]string, 0, len(p.Models))
		for _, m := range p.Models {
			tag := ""
			if m.Reasoning {
				tag = "*"
			}
			ids = append(ids, m.ID+tag)
		}
		sort.Strings(ids)
		fmt.Fprintf(w, "  models  : %s   (* = reasoning/effort)\n", strings.Join(ids, ", "))
		fmt.Fprintf(w, "  effort  : %s = {%s}\n", p.Effort.Param, strings.Join(p.Effort.Levels, ", "))
		fmt.Fprintf(w, "            %s\n", p.Effort.Summary)
		fmt.Fprintf(w, "  docs    : %s\n\n", p.DocURL)
	}
}
