package corral

import (
	"strings"
	"testing"
)

// TestCLIProvider_PromptDelivery pins the item-#9 behavior: an oversized prompt
// is delivered on stdin ONLY for providers whose CLI reads it there; for a
// PromptFlag provider that does not, it is a loud error rather than a silently
// dropped flag (which would emit e.g. `grok --single` with no value).
func TestCLIProvider_PromptDelivery(t *testing.T) {
	small := "hello"
	big := strings.Repeat("x", maxArgPrompt+1)

	stdinCapable := &CLIProvider{ProviderName: "codex", PromptStdin: true}
	flagOnly := &CLIProvider{ProviderName: "grok", PromptFlag: "--single"} // PromptStdin false

	// Small prompt → passed as an argv value, no stdin, for every provider.
	if a, useStdin, err := flagOnly.promptDelivery(small); err != nil || useStdin || a != small {
		t.Fatalf("small: arg=%q stdin=%v err=%v; want arg=%q stdin=false err=nil", a, useStdin, err, small)
	}

	// Oversized + stdin-capable → routed to stdin, no argv value.
	if a, useStdin, err := stdinCapable.promptDelivery(big); err != nil || !useStdin || a != "" {
		t.Fatalf("big+stdin: arg=%q stdin=%v err=%v; want arg=\"\" stdin=true err=nil", a, useStdin, err)
	}

	// Oversized + NOT stdin-capable → error, not a silently dropped flag.
	a, useStdin, err := flagOnly.promptDelivery(big)
	if err == nil {
		t.Fatal("oversized prompt for a non-stdin provider must error, not silently drop the flag")
	}
	if useStdin || a != "" {
		t.Errorf("error path should not route to stdin or an argv value; got arg=%q stdin=%v", a, useStdin)
	}
	if !strings.Contains(err.Error(), "grok") || !strings.Contains(err.Error(), "stdin") {
		t.Errorf("error should name the provider and the stdin limitation: %v", err)
	}
}
