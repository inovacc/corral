/*
Copyright (c) 2026 inovacc
*/

// Package gemini implements the Gemini CLI aihost.Vendor: it renders a
// component's vendor-neutral IR (aihost.Component) into a Gemini extension
// tree — skills/, gemini-extension.json, and GEMINI.md. Gemini has no native
// command/agent or hook surface, so commands and agents are surfaced as
// portable library skills (a Markdown index) instead, and hook assets are
// skipped entirely.
package gemini

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

// Host is the Gemini CLI aihost.Vendor implementation.
type Host struct{}

// Name identifies this vendor in the aihost.Vendor registry.
func (Host) Name() string { return "gemini" }

// Plugin renders the Gemini extension tree from the component IR.
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
			// Gemini has no hook surface — hook assets are intentionally
			// skipped (not rendered as any file).
		default:
			return nil, fmt.Errorf("gemini: unknown asset kind %q for %q", a.Kind, a.Name)
		}
	}

	tree["skills/"+c.Name+"-command-library/SKILL.md"] = libraryMarkdown(c.Name, "command", commandAssets)
	tree["skills/"+c.Name+"-agent-library/SKILL.md"] = libraryMarkdown(c.Name, "agent", agentAssets)

	extJSON, err := extensionManifest(c)
	if err != nil {
		return nil, err
	}
	tree["gemini-extension.json"] = extJSON

	tree["GEMINI.md"] = contextFile(c)

	return tree, nil
}

// renderMarkdown renders a YAML frontmatter block (in the given key order)
// followed by a body. The frontmatter block always ends with a newline so
// the closing "---" is on its own line.
func renderMarkdown(keys []string, values map[string]string, body string) []byte {
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
	b.WriteString(body)
	if !strings.HasSuffix(b.String(), "\n") {
		b.WriteString("\n")
	}
	return []byte(b.String())
}

// skillMarkdown renders a skill asset: name, description.
func skillMarkdown(a aihost.Asset) []byte {
	values := map[string]string{
		"name":        a.Name,
		"description": yamlScalar(a.Description),
	}
	return renderMarkdown([]string{"name", "description"}, values, a.PromptBody())
}

// libraryMarkdown renders a portable library SKILL.md: a Markdown index of
// the given assets (commands or agents), since Gemini has no native surface
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
	body.WriteString("\nGemini has no native command/agent surface, so these ship as a readable skill index instead. Adapt each listed asset's prompt to this host's conventions as needed.\n")

	values := map[string]string{
		"name":        name,
		"description": yamlScalar(description),
	}
	return renderMarkdown([]string{"name", "description"}, values, body.String())
}

// yamlScalar quotes a frontmatter scalar when it contains characters that
// would otherwise break YAML parsing.
func yamlScalar(s string) string {
	if strings.ContainsAny(s, ":#") {
		return `"` + strings.ReplaceAll(s, `"`, `\"`) + `"`
	}
	return s
}

// extensionManifest renders gemini-extension.json for the component.
func extensionManifest(c *aihost.Component) ([]byte, error) {
	type server struct {
		Command string   `json:"command"`
		Args    []string `json:"args"`
	}
	doc := struct {
		Name            string            `json:"name"`
		Version         string            `json:"version"`
		Description     string            `json:"description"`
		ContextFileName string            `json:"contextFileName"`
		MCPServers      map[string]server `json:"mcpServers"`
	}{
		Name:            c.Name,
		Version:         "0.1.0",
		Description:     c.Description,
		ContextFileName: "GEMINI.md",
		MCPServers: map[string]server{
			c.Name: {Command: c.MCP.Command, Args: c.MCP.Args},
		},
	}
	out, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("gemini: marshal gemini-extension.json: %w", err)
	}
	return append(out, '\n'), nil
}

// contextFile renders GEMINI.md: a short context file with the component
// description and a note on where bundled commands/agents live.
func contextFile(c *aihost.Component) []byte {
	var b strings.Builder
	fmt.Fprintf(&b, "# %s\n\n", c.Name)
	if c.Description != "" {
		fmt.Fprintf(&b, "%s\n\n", c.Description)
	}
	fmt.Fprintf(&b, "Bundled commands and agents live in the portable library skills: `skills/%s-command-library/` and `skills/%s-agent-library/`.\n", c.Name, c.Name)
	return []byte(b.String())
}
