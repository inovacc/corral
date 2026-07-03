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
			rt.Logger.InfoContext(ctx, "agents starting",
				slog.String("greeting", cfg.Greeting))
			// TODO(app): real work goes here.
			return rt.Shutdown(ctx)
		})
	}

	if err := root.Execute(); err != nil {
		os.Exit(1)
	}
}
