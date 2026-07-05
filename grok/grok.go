package grok

import (
	"context"
	"time"

	"github.com/inovacc/corral"
)

func init() {
	corral.RegisterProvider(func() corral.Provider { return NewProvider() }, "grok", "xai")
}

// New returns the x.ai Grok provider preset: headless single-turn mode
// (`grok --single <prompt>`), which prints the response to stdout and exits.
// An Agent.Model hint overrides; there is no provider default model so Grok
// picks its own.
func New() *corral.CLIProvider {
	return &corral.CLIProvider{
		ProviderName: "grok",
		Bin:          "grok",
		PromptFlag:   "--single", // -p/--single <PROMPT>: single-turn, prints to stdout and exits
		ModelFlag:    "--model",
		// SchemaFlag intentionally empty: grok's --json-schema takes inline JSON,
		// not a file path, so schemas are embedded in the prompt and the JSON is
		// parsed from stdout (as the claude preset does).
	}
}

// Provider is the registered Grok provider: the CLIProvider preset plus the
// corral.UsageReporter capability (the real GET /billing?format=credits call).
// Embedding promotes Name/Run/Open, so it is also a SessionOpener like the bare
// preset.
type Provider struct{ *corral.CLIProvider }

// NewProvider returns the Grok provider with subscription-usage monitoring.
func NewProvider() *Provider { return &Provider{CLIProvider: New()} }

// Usage satisfies corral.UsageReporter via the real Grok billing endpoint. A
// short timeout bounds the monitor; absence (not logged in, offline, or a
// billing error) is reported as (nil, nil) so it never blocks the fleet.
func (p *Provider) Usage(ctx context.Context) (*corral.LimitStatus, error) {
	ctx, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()
	return FetchUsage(ctx)
}
