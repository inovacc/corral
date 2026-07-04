/*
Copyright (c) 2026 inovacc
*/

// Package claude implements the Claude Code aihost.Vendor: it renders a
// component's vendor-neutral IR (aihost.Component) into a full Claude plugin
// tree — commands/, agents/, skills/, hooks/hooks.json, .mcp.json, and
// .claude-plugin/plugin.json.
package claude

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/inovacc/corral/aihost"
)

func init() {
	aihost.RegisterVendor(func() aihost.Vendor { return Host{} })
}

// Host is the Claude Code aihost.Vendor implementation.
type Host struct{}

// Name identifies this vendor in the aihost.Vendor registry.
func (Host) Name() string { return "claude" }

// InstallTarget returns the shared marketplaces dir under base; the caller
// namespaces the component's own subdir beneath it.
func (Host) InstallTarget(base string) (string, error) {
	return filepath.Join(base, ".claude", "plugins", "marketplaces"), nil
}

// Plugin renders the full Claude plugin tree from the component IR.
func (Host) Plugin(c *aihost.Component) (map[string][]byte, error) {
	tree := map[string][]byte{}

	var hookAssets []aihost.Asset

	for _, a := range c.Assets {
		switch a.Kind {
		case aihost.KindCommand:
			tree["commands/"+a.Name+".md"] = commandMarkdown(a)
		case aihost.KindAgent, aihost.KindSubagent:
			tree["agents/"+a.Name+".md"] = agentMarkdown(a)
		case aihost.KindSkill:
			tree["skills/"+a.Name+"/SKILL.md"] = skillMarkdown(a)
		case aihost.KindHook:
			hookAssets = append(hookAssets, a)
		default:
			return nil, fmt.Errorf("claude: unknown asset kind %q for %q", a.Kind, a.Name)
		}
	}

	hooksJSON, err := hooksManifest(hookAssets)
	if err != nil {
		return nil, err
	}
	tree["hooks/hooks.json"] = hooksJSON

	mcpJSON, err := mcpManifest(c)
	if err != nil {
		return nil, err
	}
	tree[".mcp.json"] = mcpJSON

	pluginJSON, err := pluginManifest(c)
	if err != nil {
		return nil, err
	}
	tree[".claude-plugin/plugin.json"] = pluginJSON

	return tree, nil
}

// renderMarkdown renders a YAML frontmatter block (in the given key order)
// followed by the asset's prompt body. The frontmatter block always ends
// with a newline so the closing "---" is on its own line.
func renderMarkdown(a aihost.Asset, keys []string, values map[string]string) []byte {
	var b strings.Builder
	b.WriteString("---\n")
	for _, k := range keys {
		v, ok := values[k]
		if !ok {
			continue
		}
		fmt.Fprintf(&b, "%s: %s\n", k, v)
	}
	b.WriteString("---\n\n")
	b.WriteString(a.PromptBody())
	if !strings.HasSuffix(b.String(), "\n") {
		b.WriteString("\n")
	}
	return []byte(b.String())
}

// commandMarkdown renders a command asset: description, argument-hint,
// allowed-tools from Metadata. No "name" key.
func commandMarkdown(a aihost.Asset) []byte {
	values := map[string]string{"description": yamlScalar(a.Description)}
	if hint, ok := metaString(a.Metadata, "argumentHint", "argument-hint"); ok {
		values["argument-hint"] = yamlScalar(hint)
	}
	if tools, ok := metaString(a.Metadata, "allowedTools", "allowed-tools"); ok {
		values["allowed-tools"] = yamlScalar(tools)
	}
	return renderMarkdown(a, []string{"description", "argument-hint", "allowed-tools"}, values)
}

// agentMarkdown renders an agent/subagent asset: name, description.
func agentMarkdown(a aihost.Asset) []byte {
	values := map[string]string{
		"name":        a.Name,
		"description": yamlScalar(a.Description),
	}
	return renderMarkdown(a, []string{"name", "description"}, values)
}

// skillMarkdown renders a skill asset: name, description.
func skillMarkdown(a aihost.Asset) []byte {
	values := map[string]string{
		"name":        a.Name,
		"description": yamlScalar(a.Description),
	}
	return renderMarkdown(a, []string{"name", "description"}, values)
}

// metaString looks up any of the given keys in metadata and returns the
// first match as a string.
func metaString(metadata map[string]any, keys ...string) (string, bool) {
	for _, k := range keys {
		if v, ok := metadata[k]; ok {
			if s, ok := v.(string); ok {
				return s, true
			}
			return fmt.Sprint(v), true
		}
	}
	return "", false
}

// yamlScalar quotes a frontmatter scalar when it contains characters that
// would otherwise break YAML parsing.
func yamlScalar(s string) string {
	if strings.ContainsAny(s, ":#") {
		return `"` + strings.ReplaceAll(s, `"`, `\"`) + `"`
	}
	return s
}

// hookEntry is one entry in a hooks.json event array.
type hookEntry struct {
	Matcher string        `json:"matcher,omitempty"`
	Hooks   []hookCommand `json:"hooks"`
}

type hookCommand struct {
	Type    string `json:"type"`
	Command string `json:"command"`
}

// hooksManifest aggregates all KindHook assets into hooks/hooks.json, keyed
// by event. encoding/json sorts map keys on marshal, so key order is
// deterministic without extra bookkeeping.
func hooksManifest(assets []aihost.Asset) ([]byte, error) {
	byEvent := map[string][]hookEntry{}
	for _, a := range assets {
		event, _ := metaString(a.Metadata, "event")
		if event == "" {
			return nil, fmt.Errorf("claude: hook %q missing metadata.event", a.Name)
		}
		command, _ := metaString(a.Metadata, "command")
		if command == "" {
			return nil, fmt.Errorf("claude: hook %q missing metadata.command", a.Name)
		}
		entry := hookEntry{Hooks: []hookCommand{{Type: "command", Command: command}}}
		if matcher, ok := metaString(a.Metadata, "matcher"); ok {
			entry.Matcher = matcher
		}
		byEvent[event] = append(byEvent[event], entry)
	}

	doc := struct {
		Hooks map[string][]hookEntry `json:"hooks"`
	}{Hooks: byEvent}
	out, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("claude: marshal hooks.json: %w", err)
	}
	return append(out, '\n'), nil
}

// mcpManifest renders .mcp.json for the component's MCP server.
func mcpManifest(c *aihost.Component) ([]byte, error) {
	type server struct {
		Command string   `json:"command"`
		Args    []string `json:"args"`
	}
	doc := struct {
		MCPServers map[string]server `json:"mcpServers"`
	}{
		MCPServers: map[string]server{
			c.Name: {Command: c.MCP.Command, Args: c.MCP.Args},
		},
	}
	out, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("claude: marshal .mcp.json: %w", err)
	}
	return append(out, '\n'), nil
}

// pluginManifest renders .claude-plugin/plugin.json.
func pluginManifest(c *aihost.Component) ([]byte, error) {
	doc := struct {
		Name        string            `json:"name"`
		Version     string            `json:"version"`
		Description string            `json:"description"`
		Author      map[string]string `json:"author"`
		License     string            `json:"license"`
		Commands    string            `json:"commands"`
		Agents      string            `json:"agents"`
		Skills      string            `json:"skills"`
		MCPServers  string            `json:"mcpServers"`
	}{
		Name:        c.Name,
		Version:     "0.1.0",
		Description: c.Description,
		Author:      map[string]string{"name": c.Name},
		License:     "BSD-3-Clause",
		Commands:    "commands",
		Agents:      "agents",
		Skills:      "skills",
		MCPServers:  ".mcp.json",
	}
	out, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("claude: marshal plugin.json: %w", err)
	}
	return append(out, '\n'), nil
}
