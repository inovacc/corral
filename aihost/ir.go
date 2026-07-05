/*
Copyright (c) 2026 inovacc
*/

// Package aihost is corral's cross-vendor component generator: it loads one
// canonical component spec (corral.json) into a vendor-neutral IR and generates,
// per AI vendor (Claude/Gemini/Codex), a complete component — an installable
// plugin tree plus a Go module wrapper. Codegen mirrors sequa's engine: only the
// per-vendor plugin tree varies (behind the Vendor interface); the IR and the
// Go-module codegen are shared.
package aihost

import (
	"encoding/json"
	"fmt"
	"os"
	"regexp"
)

var envNameRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

type AssetKind string

const (
	KindCommand  AssetKind = "command"
	KindAgent    AssetKind = "agent"
	KindSubagent AssetKind = "subagent"
	KindSkill    AssetKind = "skill"
	KindHook     AssetKind = "hook"
)

// Asset is a vendor-neutral definition of one command/agent/subagent/skill/hook.
type Asset struct {
	Kind        AssetKind      `json:"kind"`
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Body        string         `json:"body,omitempty"`   // prompt / instructions markdown
	System      string         `json:"system,omitempty"` // agents: system prompt (alias of Body)
	Tools       []string       `json:"tools,omitempty"`
	Model       string         `json:"model,omitempty"`
	Metadata    map[string]any `json:"metadata,omitempty"` // argumentHint, allowedTools, event, matcher…
}

// PromptBody returns the effective prompt (Body, else System).
func (a Asset) PromptBody() string {
	if a.Body != "" {
		return a.Body
	}
	return a.System
}

type MCPSpec struct {
	Command string   `json:"command"`
	Args    []string `json:"args"`
}

type Component struct {
	Name, Module, Description string
	MCP                       MCPSpec
	Assets                    []Asset
	Vendors                   []string
	Executions                map[string]Execution
}

// Execution selects how a vendor's turns run: "subscription" (a coding-agent
// CLI, the default) or "api" (a metered API backend). Absent ⇒ subscription.
type Execution struct {
	Mode   string          `json:"mode"`
	Config ExecutionConfig `json:"config"`
}

// ExecutionConfig configures an "api" execution. KeyEnv is an environment
// variable NAME (never a literal key); the generated code resolves it at
// runtime via os.Getenv.
type ExecutionConfig struct {
	Format  string         `json:"format"`
	Model   string         `json:"model"`
	KeyEnv  string         `json:"key_env"`
	BaseURL string         `json:"base_url"`
	Routing map[string]any `json:"routing"`
}

func Load(path string) (*Component, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("aihost: read %s: %w", path, err)
	}
	// component sub-object fields need explicit tags; decode into a shaped struct.
	var s struct {
		Component struct {
			Name        string  `json:"name"`
			Module      string  `json:"module"`
			Description string  `json:"description"`
			MCP         MCPSpec `json:"mcp"`
		} `json:"component"`
		Vendors   []string             `json:"vendors"`
		Assets    []Asset              `json:"assets"`
		Execution map[string]Execution `json:"execution"`
	}
	if err := json.Unmarshal(raw, &s); err != nil {
		return nil, fmt.Errorf("aihost: parse %s: %w", path, err)
	}
	if s.Component.Name == "" || s.Component.Module == "" {
		return nil, fmt.Errorf("aihost: component.name and component.module are required")
	}
	for vendor, ex := range s.Execution {
		if err := validateExecution(vendor, ex); err != nil {
			return nil, err
		}
	}
	return &Component{
		Name: s.Component.Name, Module: s.Component.Module, Description: s.Component.Description,
		MCP: s.Component.MCP, Assets: s.Assets, Vendors: s.Vendors, Executions: s.Execution,
	}, nil
}

func validateExecution(vendor string, ex Execution) error {
	switch ex.Mode {
	case "", "subscription":
		return nil
	case "api":
		// validated below
	default:
		return fmt.Errorf("aihost: vendor %q: unknown execution mode %q (want subscription|api)", vendor, ex.Mode)
	}
	switch ex.Config.Format {
	case "openrouter", "openai", "anthropic":
	default:
		return fmt.Errorf("aihost: vendor %q: unknown api format %q (want openrouter|openai|anthropic)", vendor, ex.Config.Format)
	}
	if ex.Config.Model == "" {
		return fmt.Errorf("aihost: vendor %q: api execution requires config.model", vendor)
	}
	if ex.Config.KeyEnv == "" {
		return fmt.Errorf("aihost: vendor %q: api execution requires config.key_env", vendor)
	}
	if !envNameRe.MatchString(ex.Config.KeyEnv) {
		return fmt.Errorf("aihost: vendor %q: config.key_env %q must be an environment-variable NAME, not a literal key", vendor, ex.Config.KeyEnv)
	}
	return nil
}
