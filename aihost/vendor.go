/*
Copyright (c) 2026 inovacc
*/

package aihost

import (
	"bytes"
	"fmt"
	"go/format"
	"sort"
	"strings"
	"sync"
	"text/template"
)

// Vendor is a codegen backend for one AI host (sequa's Engine). Only the
// installable plugin tree varies per vendor; the Go-module wrapper is shared.
type Vendor interface {
	Name() string
	Plugin(c *Component) (map[string][]byte, error) // slash-path → bytes
}

var (
	vendorMu  sync.RWMutex
	vendorFac = map[string]func() Vendor{}
)

// RegisterVendor registers a vendor factory (call from init()). Unlike sequa's
// compiled-in switch, this is a registry so users add vendors without forking.
func RegisterVendor(f func() Vendor) {
	v := f()
	vendorMu.Lock()
	vendorFac[v.Name()] = f
	vendorMu.Unlock()
}

func Vendors() []Vendor {
	vendorMu.RLock()
	defer vendorMu.RUnlock()
	out := make([]Vendor, 0, len(vendorFac))
	for _, f := range vendorFac {
		out = append(out, f())
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name() < out[j].Name() })
	return out
}

func VendorByName(name string) (Vendor, bool) {
	vendorMu.RLock()
	f, ok := vendorFac[name]
	vendorMu.RUnlock()
	if !ok {
		return nil, false
	}
	return f(), true
}

// GeneratedFile is one file the caller writes to disk.
type GeneratedFile struct {
	Path    string
	Content []byte
}

// renderFormatted executes a Go-source template and runs it through go/format —
// so generated Go is always gofmt-clean and syntactically valid (sequa's guard).
func renderFormatted(t *template.Template, data any) ([]byte, error) {
	var buf bytes.Buffer
	if err := t.Execute(&buf, data); err != nil {
		return nil, fmt.Errorf("aihost: template %s: %w", t.Name(), err)
	}
	out, err := format.Source(buf.Bytes())
	if err != nil {
		return nil, fmt.Errorf("aihost: gofmt generated %s: %w\n%s", t.Name(), err, buf.String())
	}
	return out, nil
}

// RenderMarkdown renders a YAML frontmatter block (from values, in the given
// key order) followed by a blank line and body. This is the one shared
// frontmatter renderer for all vendors (claude/gemini/codex previously each
// carried their own copy). Callers pass raw Metadata/field values — string,
// []string, or a JSON-decoded []interface{} — and RenderMarkdown formats
// each via YAMLScalar; keys absent from values are skipped. The frontmatter
// block ALWAYS ends with a newline so the closing --- is on its own line
// (the Render invariant carried from unravel's aihost).
func RenderMarkdown(keys []string, values map[string]any, body string) []byte {
	var b strings.Builder
	b.WriteString("---\n")
	for _, k := range keys {
		v, ok := values[k]
		if !ok {
			continue
		}
		fmt.Fprintf(&b, "%s: %s\n", k, YAMLScalar(v))
	}
	b.WriteString("---\n\n")
	b.WriteString(body)
	if !strings.HasSuffix(b.String(), "\n") {
		b.WriteString("\n")
	}
	return []byte(b.String())
}

// YAMLScalar renders a frontmatter value as a minimal YAML scalar: a bare or
// quoted string, a flow list ("[a, b, c]") for []string or []interface{}, or
// a fmt.Sprint fallback for anything else.
//
// The []interface{} branch matters: Asset.Metadata is decoded from JSON, so
// an array-valued key (e.g. `allowed-tools: [Bash, Read]` in corral.json)
// unmarshals to []interface{}, not []string. Without this branch it falls
// through to fmt.Sprint(t), which renders "[Bash Read]" — invalid YAML
// frontmatter (space-separated, no commas, no quoting). This was the
// metaString/yamlScalar bug in the vendor packages: they type-asserted
// Metadata values as string and silently stringified arrays before the
// scalar formatter ever saw them.
func YAMLScalar(v any) string {
	switch t := v.(type) {
	case string:
		return yamlScalarString(t)
	case []string:
		return yamlScalarList(t)
	case []interface{}:
		return yamlScalarList(t)
	default:
		return fmt.Sprint(t)
	}
}

// yamlScalarList renders a flow-list ("[a, b, c]") from any slice, quoting
// string elements as needed. Shared by the []string and []interface{}
// branches of YAMLScalar so both render identically.
func yamlScalarList[T any](items []T) string {
	parts := make([]string, len(items))
	for i, it := range items {
		if s, ok := any(it).(string); ok {
			parts[i] = yamlScalarString(s)
		} else {
			parts[i] = fmt.Sprint(it)
		}
	}
	return "[" + strings.Join(parts, ", ") + "]"
}

// yamlScalarString quotes a scalar string when it contains characters that
// would otherwise break YAML parsing (':' or '#'), or has leading/trailing
// whitespace; otherwise it is returned bare.
func yamlScalarString(s string) string {
	if strings.ContainsAny(s, ":#") || s != strings.TrimSpace(s) {
		return `"` + strings.ReplaceAll(s, `"`, `\"`) + `"`
	}
	return s
}
