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
func (fakeVendor) InstallTarget(base string) (string, error) { return base + "/fake", nil }

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

func TestAssetMarkdown_FrontmatterEndsWithNewline(t *testing.T) {
	md := assetMarkdown(Asset{Name: "n", Description: "d", Body: "# body"}, map[string]any{"description": "d"})
	s := string(md)
	if !strings.HasPrefix(s, "---\n") {
		t.Fatal("no opening frontmatter")
	}
	// The closing --- must be on its own line (frontmatter block ends with \n).
	if !strings.Contains(s, "\n---\n") {
		t.Fatalf("closing delimiter merged / missing:\n%s", s)
	}
}
