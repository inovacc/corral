package all

import (
	"os"
	"testing"

	"github.com/inovacc/corral"
)

// TestProvidersRegistered asserts the barrel import wired every vendor provider
// into the core registry — the contract the rest of the runtime relies on.
func TestProvidersRegistered(t *testing.T) {
	for _, name := range []string{"codex", "claude", "code", "agy", "antigravity"} {
		if _, err := corral.ProviderByName(name); err != nil {
			t.Errorf("provider %q not registered: %v", name, err)
		}
	}
	if _, err := corral.ProviderByName(""); err != nil {
		t.Errorf("default provider unresolved: %v", err)
	}
}

// TestAgencyRoundTripCodex is the real end-to-end path: Agency -> registry ->
// codex CLIProvider -> `codex exec`. It is OPT-IN (AGENTS_E2E=1) so it never
// burns Codex quota in CI or a normal run.
func TestAgencyRoundTripCodex(t *testing.T) {
	if os.Getenv("AGENTS_E2E") != "1" {
		t.Skip("set AGENTS_E2E=1 to run the live codex round-trip")
	}
	ag, err := corral.NewAgency("codex", ".")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ag.Close() }()
	res, err := ag.RunAgent(t.Context(),
		corral.Agent{Name: "probe", System: "Reply with exactly the word READY and nothing else."},
		"")
	if err != nil {
		t.Fatalf("agency run: %v", err)
	}
	if res.Text == "" {
		t.Error("empty result from codex")
	}
}
