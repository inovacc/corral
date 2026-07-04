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
)

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
		Vendors []string `json:"vendors"`
		Assets  []Asset  `json:"assets"`
	}
	if err := json.Unmarshal(raw, &s); err != nil {
		return nil, fmt.Errorf("aihost: parse %s: %w", path, err)
	}
	if s.Component.Name == "" || s.Component.Module == "" {
		return nil, fmt.Errorf("aihost: component.name and component.module are required")
	}
	return &Component{
		Name: s.Component.Name, Module: s.Component.Module, Description: s.Component.Description,
		MCP: s.Component.MCP, Assets: s.Assets, Vendors: s.Vendors,
	}, nil
}
