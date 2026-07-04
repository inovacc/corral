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

// assetMarkdown renders an Asset to a vendor markdown file: YAML frontmatter
// (from the provided fields, in stable key order) + blank line + body. The
// frontmatter block ALWAYS ends with a newline so the closing --- is on its own
// line (the Render invariant carried from unravel's aihost).
func assetMarkdown(a Asset, frontmatter map[string]any) []byte {
	var b strings.Builder
	b.WriteString("---\n")
	keys := make([]string, 0, len(frontmatter))
	for k := range frontmatter {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		fmt.Fprintf(&b, "%s: %s\n", k, yamlScalar(frontmatter[k]))
	}
	b.WriteString("---\n\n")
	b.WriteString(a.PromptBody())
	if !strings.HasSuffix(b.String(), "\n") {
		b.WriteString("\n")
	}
	return []byte(b.String())
}

// yamlScalar renders a frontmatter value as a minimal YAML scalar (string,
// []string as a flow list, else fmt).
func yamlScalar(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case []string:
		return "[" + strings.Join(t, ", ") + "]"
	default:
		return fmt.Sprint(t)
	}
}
