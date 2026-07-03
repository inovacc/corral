package qwen

import "github.com/inovacc/corral"

func init() {
	corral.RegisterProvider(func() corral.Provider { return New() }, "qwen", "qwen-code")
}

// New returns the Alibaba Qwen Code provider preset: headless
// `qwen --prompt <prompt>` (a gemini-cli fork's non-interactive mode),
// auto-approving tools with --yolo. Qwen requires an auth type configured
// (`--auth-type` / settings.json) before non-interactive runs succeed; it also
// reads the prompt from stdin, so oversized prompts route there.
func New() *corral.CLIProvider {
	return &corral.CLIProvider{
		ProviderName: "qwen",
		Bin:          "qwen",
		BaseArgs:     []string{"--yolo"}, // -y/--yolo: auto-approve (non-interactive)
		PromptFlag:   "--prompt",         // -p/--prompt <string>: non-interactive prompt (also reads stdin)
		ModelFlag:    "--model",
	}
}
