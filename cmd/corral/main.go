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

// isLightCommand reports whether a subcommand should bypass the mantle bootstrap
// (read-only/interactive commands whose stdout must stay clean).
func isLightCommand(arg string) bool {
	switch arg {
	case "usage", "launch", "serve":
		return true
	}
	return false
}

func main() {
	// Read-only/interactive commands (usage, launch) bypass the mantle bootstrap:
	// its config loader logs to stdout and writes a config.yaml in the cwd, which
	// would pollute a command whose stdout must be pure data or an inherited TTY.
	if len(os.Args) > 1 && isLightCommand(os.Args[1]) {
		light := &cobra.Command{Use: "corral", Version: version}
		light.AddCommand(newUsageCmd(), newLaunchCmd(), newServeCmd())
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

	root.AddCommand(newUsageCmd(), newLaunchCmd(), newServeCmd())

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
