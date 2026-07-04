/*
Copyright (c) 2026 inovacc
*/

// Package codex implements the Codex CLI aihost.Vendor: it renders a
// component's vendor-neutral IR (aihost.Component) into a Codex plugin
// tree — skills/, .mcp.json, and .codex-plugin/plugin.json. Codex has no
// native command/agent or hook surface, so commands and agents are surfaced
// as portable library skills (a Markdown index) instead, and hook assets are
// skipped entirely.
package codex

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/inovacc/corral/aihost"
)

func init() {
	aihost.RegisterVendor(func() aihost.Vendor { return Host{} })
}

// Host is the Codex CLI aihost.Vendor implementation.
type Host struct{}

// Name identifies this vendor in the aihost.Vendor registry.
func (Host) Name() string { return "codex" }

// Plugin renders the Codex plugin tree from the component IR.
func (Host) Plugin(c *aihost.Component) (map[string][]byte, error) {
	tree := map[string][]byte{}

	var commandAssets, agentAssets []aihost.Asset

	for _, a := range c.Assets {
		switch a.Kind {
		case aihost.KindSkill:
			tree["skills/"+a.Name+"/SKILL.md"] = skillMarkdown(a)
		case aihost.KindCommand:
			commandAssets = append(commandAssets, a)
		case aihost.KindAgent, aihost.KindSubagent:
			agentAssets = append(agentAssets, a)
		case aihost.KindHook:
			// Codex has no hook surface — hook assets are intentionally
			// skipped (not rendered as any file).
		default:
			return nil, fmt.Errorf("codex: unknown asset kind %q for %q", a.Kind, a.Name)
		}
	}

	tree["skills/"+c.Name+"-command-library/SKILL.md"] = libraryMarkdown(c.Name, "command", commandAssets)
	tree["skills/"+c.Name+"-agent-library/SKILL.md"] = libraryMarkdown(c.Name, "agent", agentAssets)

	pluginJSON, err := pluginManifest(c)
	if err != nil {
		return nil, err
	}
	tree[".codex-plugin/plugin.json"] = pluginJSON

	mcpJSON, err := mcpManifest(c)
	if err != nil {
		return nil, err
	}
	tree[".mcp.json"] = mcpJSON

	return tree, nil
}

// skillMarkdown renders a skill asset: name, description.
func skillMarkdown(a aihost.Asset) []byte {
	values := map[string]any{"name": a.Name, "description": a.Description}
	return aihost.RenderMarkdown([]string{"name", "description"}, values, a.PromptBody())
}

// libraryMarkdown renders a portable library SKILL.md: a Markdown index of
// the given assets (commands or agents), since Codex has no native surface
// for either. component is used as the name prefix; kindLabel is "command"
// or "agent" and appears in the skill name/description and index entries.
func libraryMarkdown(component, kindLabel string, assets []aihost.Asset) []byte {
	name := component + "-" + kindLabel + "-library"
	description := fmt.Sprintf("Portable index of %s assets for %s, adapted for hosts without a native %s surface.", kindLabel, component, kindLabel)

	sorted := make([]aihost.Asset, len(assets))
	copy(sorted, assets)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Name < sorted[j].Name })

	var body strings.Builder
	body.WriteString("## Index\n\n")
	for _, a := range sorted {
		fmt.Fprintf(&body, "- %s (%s): %s\n", a.Name, a.Kind, a.Description)
	}
	body.WriteString("\nCodex has no native command/agent surface, so these ship as a readable skill index instead. Adapt each listed asset's prompt to this host's conventions as needed.\n")

	values := map[string]any{"name": name, "description": description}
	return aihost.RenderMarkdown([]string{"name", "description"}, values, body.String())
}

// pluginManifest renders .codex-plugin/plugin.json.
func pluginManifest(c *aihost.Component) ([]byte, error) {
	doc := struct {
		Name        string            `json:"name"`
		Version     string            `json:"version"`
		Description string            `json:"description"`
		Author      map[string]string `json:"author"`
		License     string            `json:"license"`
		Skills      string            `json:"skills"`
		MCPServers  string            `json:"mcpServers"`
	}{
		Name:        c.Name,
		Version:     "0.1.0",
		Description: c.Description,
		Author:      map[string]string{"name": c.Name},
		License:     "BSD-3-Clause",
		Skills:      "skills",
		MCPServers:  ".mcp.json",
	}
	out, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("codex: marshal plugin.json: %w", err)
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
		return nil, fmt.Errorf("codex: marshal .mcp.json: %w", err)
	}
	return append(out, '\n'), nil
}
