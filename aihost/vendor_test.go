package aihost

import (
	"strings"
	"testing"
)

type fakeVendor struct{}

func (fakeVendor) Name() string { return "fake" }
func (fakeVendor) Plugin(*Component) (map[string][]byte, error) {
	return map[string][]byte{"x.md": []byte("x")}, nil
}
func TestVendorRegistry(t *testing.T) {
	RegisterVendor(func() Vendor { return fakeVendor{} })
	if _, ok := VendorByName("fake"); !ok {
		t.Fatal("fake vendor not registered")
	}
	found := false
	for _, v := range Vendors() {
		if v.Name() == "fake" {
			found = true
		}
	}
	if !found {
		t.Fatal("fake not in Vendors()")
	}
}

func TestRenderMarkdown_FrontmatterEndsWithNewline(t *testing.T) {
	md := RenderMarkdown([]string{"description"}, map[string]any{"description": "d"}, "# body")
	s := string(md)
	if !strings.HasPrefix(s, "---\n") {
		t.Fatal("no opening frontmatter")
	}
	// The closing --- must be on its own line (frontmatter block ends with \n).
	if !strings.Contains(s, "\n---\n") {
		t.Fatalf("closing delimiter merged / missing:\n%s", s)
	}
}

func TestYAMLScalar(t *testing.T) {
	tests := []struct {
		name string
		in   any
		want string
	}{
		{"plain string stays bare", "Bash", "Bash"},
		{"string with colon gets quoted", "a: b", `"a: b"`},
		{"[]string flow list", []string{"Bash", "Read"}, "[Bash, Read]"},
		{
			"[]interface{} flow list matches []string (the JSON-decode bug fix)",
			[]interface{}{"Bash", "Read"}, "[Bash, Read]",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := YAMLScalar(tt.in); got != tt.want {
				t.Errorf("YAMLScalar(%#v) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}
