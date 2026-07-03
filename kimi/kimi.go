package kimi

import "github.com/inovacc/corral"

func init() {
	corral.RegisterProvider(func() corral.Provider { return New() }, "kimi", "kimi-code", "moonshot")
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
		ModelFlag:    "--model",
	}
}
