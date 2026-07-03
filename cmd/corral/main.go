package main

import (
	"context"
	"log/slog"
	"os"

	"github.com/spf13/cobra"

	"github.com/inovacc/mantle/bootstrap"

	"github.com/inovacc/corral/internal/app"
)

var version = "dev"

func main() {
	// Read-only query commands (usage) bypass the mantle bootstrap: its config
	// loader logs to stdout and writes a config.yaml in the cwd, which would
	// pollute a command whose stdout must be pure, pipeable data.
	if len(os.Args) > 1 && os.Args[1] == "usage" {
		light := &cobra.Command{Use: "corral", Version: version}
		light.AddCommand(newUsageCmd())
		if err := light.Execute(); err != nil {
			os.Exit(1)
		}
		return
	}

	root := &cobra.Command{
		Use:   "corral",
		Short: "corral",
	}

	a := app.New()

	if err := bootstrap.Configure(root, a,
		bootstrap.WithAppName("corral"),
		bootstrap.WithVersion(version),
	); err != nil {
		_, _ = os.Stderr.WriteString(err.Error() + "\n")
		os.Exit(1)
	}

	root.AddCommand(newUsageCmd())

	root.RunE = func(cmd *cobra.Command, _ []string) error {
		return bootstrap.Run(cmd, func(ctx context.Context, rt *bootstrap.Runtime) error {
			cfg := bootstrap.ConfigOf[*app.App](rt)
			rt.Logger.InfoContext(ctx, "corral starting",
				slog.String("greeting", cfg.Greeting))
			// TODO(app): real work goes here.
			return rt.Shutdown(ctx)
		})
	}

	if err := root.Execute(); err != nil {
		os.Exit(1)
	}
}
