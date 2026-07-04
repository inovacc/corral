package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/inovacc/corral"
	_ "github.com/inovacc/corral/all" // register providers
)

// serveWatch pairs a display name with its usage reporter.
type serveWatch struct {
	name     string
	reporter corral.UsageReporter
}

// serveOpts are the recorder loop knobs.
type serveOpts struct {
	interval  time.Duration
	threshold float64
	once      bool
}

// resolveServeWatches turns provider names into watchable (name, reporter) pairs,
// skipping unknown providers and those without a usage endpoint (returned as
// human-readable skip reasons for the caller to surface).
func resolveServeWatches(names []string) (watches []serveWatch, skipped []string) {
	for _, name := range names {
		p, err := corral.ProviderByName(name)
		if err != nil {
			skipped = append(skipped, name+" (unknown provider)")
			continue
		}
		ur, ok := p.(corral.UsageReporter)
		if !ok {
			skipped = append(skipped, p.Name()+" (no usage endpoint)")
			continue
		}
		watches = append(watches, serveWatch{name: p.Name(), reporter: ur})
	}
	return watches, skipped
}

// runServe is the testable recorder core: wire the Monitor to the sink and run
// (once, or until ctx is cancelled), then flush the sink.
func runServe(ctx context.Context, watches []serveWatch, sink corral.UsageSink, opts serveOpts) error {
	mon := corral.NewMonitor(
		corral.WithInterval(opts.interval),
		corral.WithThreshold(opts.threshold),
		corral.OnSample(func(s corral.Sample) {
			if err := sink.WriteSample(s); err != nil {
				slog.Error("serve: sink write sample", "provider", s.Provider, "err", err)
			}
		}),
		corral.OnAlert(func(a corral.Alert) {
			slog.Warn("usage threshold crossed", "provider", a.Provider, "worst_pct", a.Worst, "threshold", a.Threshold)
			if err := sink.WriteAlert(a); err != nil {
				slog.Error("serve: sink write alert", "provider", a.Provider, "err", err)
			}
		}),
	)
	for _, w := range watches {
		mon.Watch(w.name, w.reporter)
	}
	if opts.once {
		mon.Poll(ctx)
	} else {
		mon.Run(ctx) // blocks until ctx cancelled
	}
	return sink.Close()
}

// splitCSV splits a comma list, trimming spaces and dropping empties.
func splitCSV(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// newServeCmd builds `corral serve`.
func newServeCmd() *cobra.Command {
	var (
		out       string
		interval  time.Duration
		threshold float64
		providers string
		once      bool
	)
	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Run a usage-recorder daemon: poll provider usage and append changes to a JSONL file",
		Long: "Continuously poll each subscription provider's usage and append a JSON line to\n" +
			"--out whenever a provider's usage CHANGES (plus a line per threshold alert).\n" +
			"An idle fleet produces almost no output. Ctrl-C flushes and exits.\n" +
			"Examples: `corral serve`, `corral serve --interval 2m --out ./usage.jsonl`, `corral serve --once`.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			names := canonicalProviders()
			if strings.TrimSpace(providers) != "" {
				names = splitCSV(providers)
			}
			watches, skipped := resolveServeWatches(names)
			for _, s := range skipped {
				cmd.PrintErrf("skipping %s\n", s)
			}
			if len(watches) == 0 {
				return fmt.Errorf("no providers with a usage endpoint to watch (resolved: %v)", names)
			}

			sink, err := corral.NewJSONLSink(out)
			if err != nil {
				return fmt.Errorf("open usage log %q: %w", out, err)
			}

			ctx := context.Background()
			if !once {
				var stop context.CancelFunc
				ctx, stop = signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
				defer stop()
				cmd.PrintErrf("corral serve: recording %d provider(s) to %s every %s (Ctrl-C to stop)\n",
					len(watches), out, interval)
			}
			return runServe(ctx, watches, sink, serveOpts{interval: interval, threshold: threshold, once: once})
		},
	}
	cmd.Flags().StringVar(&out, "out", homePath(".corral", "usage.jsonl"), "JSONL output path")
	cmd.Flags().DurationVar(&interval, "interval", 5*time.Minute, "poll cadence")
	cmd.Flags().Float64Var(&threshold, "threshold", 80, "alert threshold percent (0..100; <=0 disables alerting)")
	cmd.Flags().StringVar(&providers, "providers", "", "comma-separated providers to watch (default: all with a usage endpoint)")
	cmd.Flags().BoolVar(&once, "once", false, "poll once, write, and exit")
	return cmd
}
