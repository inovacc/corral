package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/inovacc/corral/airef"
)

// newCatalogCmd is a thin wrapper over airef.Providers / airef.RenderCatalog.
func newCatalogCmd() *cobra.Command {
	var only string
	cmd := &cobra.Command{
		Use:          "catalog",
		Short:        "Show the curated AI model catalog (model IDs + reasoning-effort controls)",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			providers := airef.Providers()
			if only != "" {
				p, ok := airef.Lookup(only)
				if !ok {
					return fmt.Errorf("unknown provider %q (want anthropic|openai|gemini|openrouter)", only)
				}
				providers = []airef.Provider{p}
			}
			airef.RenderCatalog(cmd.OutOrStdout(), providers)
			return nil
		},
	}
	cmd.Flags().StringVarP(&only, "provider", "p", "", "only this provider (anthropic|openai|gemini|openrouter)")
	return cmd
}

// newSnippetCmd is a thin wrapper over airef.Snippet — a first-class command
// (no subcommands): generate a copy-paste integration snippet.
func newSnippetCmd() *cobra.Command {
	var (
		provider, lang, model, effort string
		stream                        bool
	)
	cmd := &cobra.Command{
		Use:   "snippet",
		Short: "Generate a copy-paste integration snippet for a provider + language",
		Long: "Emit ready-to-run integration code for an AI provider in the chosen language,\n" +
			"using the vendor's official SDK where one exists (REST via reqwest for Rust).\n" +
			"Keys are read from the provider's environment variable, never inlined.\n\n" +
			"Providers: anthropic | openai | gemini | openrouter\n" +
			"Languages: curl | go | python | typescript | javascript | java | rust\n\n" +
			"Examples:\n" +
			"  corral snippet -p anthropic -l curl\n" +
			"  corral snippet -p anthropic -l go -e high\n" +
			"  corral snippet -p gemini -l python -m gemini-2.5-pro\n" +
			"  corral snippet -p openrouter -l go -m openai/gpt-4o-mini",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			p, ok := airef.Lookup(provider)
			if !ok {
				return fmt.Errorf("unknown provider %q (want anthropic|openai|gemini|openrouter)", provider)
			}
			code, err := airef.Snippet(p, lang, airef.Opts{Model: model, Effort: effort, Stream: stream})
			if err != nil {
				return err
			}
			fmt.Fprint(cmd.OutOrStdout(), code)
			return nil
		},
	}
	cmd.Flags().StringVarP(&provider, "provider", "p", "anthropic", "provider: anthropic|openai|gemini|openrouter")
	cmd.Flags().StringVarP(&lang, "lang", "l", "curl", "language: curl|go|python|typescript|javascript|java|rust")
	cmd.Flags().StringVarP(&model, "model", "m", "", "model id (default: provider default)")
	cmd.Flags().StringVarP(&effort, "effort", "e", "", "reasoning effort (e.g. high); appends provider-specific guidance")
	cmd.Flags().BoolVar(&stream, "stream", false, "append streaming guidance")
	return cmd
}
