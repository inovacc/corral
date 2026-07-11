package main

import (
	"context"
	"encoding/json"
	"time"

	"github.com/spf13/cobra"

	"github.com/inovacc/corral/airef"
)

// newModelsCmd is a thin wrapper over airef.ListModels / airef.RenderModels —
// all logic lives in the module.
func newModelsCmd() *cobra.Command {
	var (
		only       string
		limit      int
		asJSON     bool
		timeoutSec int
	)
	cmd := &cobra.Command{
		Use:   "models",
		Short: "List the live models each AI provider currently serves",
		Long: "Query each provider's models endpoint and print the model IDs it serves.\n" +
			"API keys are read from each provider's environment variable\n" +
			"(ANTHROPIC_API_KEY, OPENAI_API_KEY, GEMINI_API_KEY, OPENROUTER_API_KEY);\n" +
			"a provider with no key set is skipped.",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx, cancel := context.WithTimeout(cmd.Context(), time.Duration(timeoutSec)*time.Second)
			defer cancel()

			listings := airef.ListModels(ctx, airef.EnvKey, only)
			if asJSON {
				enc := json.NewEncoder(cmd.OutOrStdout())
				enc.SetIndent("", "  ")
				return enc.Encode(listings)
			}
			airef.RenderModels(cmd.OutOrStdout(), listings, limit)
			return nil
		},
	}
	cmd.Flags().StringVarP(&only, "provider", "p", "", "only this provider (anthropic|openai|gemini|openrouter)")
	cmd.Flags().IntVar(&limit, "limit", 40, "max models to print per provider (0 = all)")
	cmd.Flags().BoolVar(&asJSON, "json", false, "emit JSON instead of text")
	cmd.Flags().IntVar(&timeoutSec, "timeout", 30, "overall timeout, in seconds")
	return cmd
}
