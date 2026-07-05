package kimi

import (
	"context"
	"time"

	"github.com/inovacc/corral"
)

func init() {
	corral.RegisterProvider(func() corral.Provider { return NewProvider() }, "kimi", "kimi-code", "moonshot")
}

// New returns the Moonshot AI Kimi Code provider preset: headless
// `kimi --prompt <prompt>` (runs one prompt non-interactively and prints the
// response), auto-approving tool executions with --yolo so a headless turn
// never blocks on a confirmation.
func New() *corral.CLIProvider {
	return &corral.CLIProvider{
		ProviderName: "kimi",
		Bin:          "kimi",
		BaseArgs:     []string{"--yolo"}, // -y/--yolo: auto-approve all actions
		PromptFlag:   "--prompt",         // -p/--prompt <prompt>: run one prompt non-interactively and print
		// PromptStdin intentionally unset (item #9): kimi's stdin handling for the
		// prompt is unverified, so an oversized prompt errors loudly rather than
		// emitting `--prompt` with no value.
		ModelFlag: "--model",
	}
}

// Provider is the registered Kimi provider: the CLIProvider preset plus the
// corral.UsageReporter capability (the real managed /usages call). Embedding
// promotes Name/Run/Open, so it is also a SessionOpener like the bare preset.
type Provider struct{ *corral.CLIProvider }

// NewProvider returns the Kimi provider with subscription-usage monitoring.
func NewProvider() *Provider { return &Provider{CLIProvider: New()} }

// Usage satisfies corral.UsageReporter via the real Kimi Code managed endpoint:
// GET {base}/usages with the OAuth bearer from ~/.kimi-code/credentials/. A
// short timeout bounds the monitor; absence (not logged in, BYOK-only, or any
// HTTP/decode error) is reported as (nil, nil) so it never blocks the fleet.
func (p *Provider) Usage(ctx context.Context) (*corral.LimitStatus, error) {
	ctx, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()
	return ReadUsage(ctx)
}
