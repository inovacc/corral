package grok

import "github.com/inovacc/corral"

func init() {
	corral.RegisterProvider(func() corral.Provider { return New() }, "grok", "xai")
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
