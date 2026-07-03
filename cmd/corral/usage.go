package main

import (
	"fmt"
	"io"
	"strings"
	"sync"

	"github.com/spf13/cobra"

	"github.com/inovacc/corral"
	_ "github.com/inovacc/corral/all" // register every provider
)

// newUsageCmd builds `corral usage [provider] [--all]`: the terminal view of the
// same subscription /usage call each coding-agent CLI makes.
func newUsageCmd() *cobra.Command {
	var all bool
	cmd := &cobra.Command{
		Use:   "usage [provider]",
		Short: "Show subscription usage/limits (each CLI's /usage) and headroom",
		Long: "Query providers' subscription usage — the same /usage call each coding-agent CLI makes —\n" +
			"and print their rate-limit windows with headroom. Pass --all for every provider, or name one\n" +
			"(e.g. `corral usage claude`). Providers with no queryable limit, or where you are not logged\n" +
			"in, are noted rather than failing.",
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			var names []string
			switch {
			case all:
				names = canonicalProviders()
			case len(args) == 1:
				names = []string{args[0]}
			default:
				return fmt.Errorf("name a provider or pass --all (registered: %s)", strings.Join(canonicalProviders(), ", "))
			}
			printUsage(cmd.OutOrStdout(), names)
			return nil
		},
	}
	cmd.Flags().BoolVar(&all, "all", false, "show every registered provider")
	return cmd
}

// canonicalProviders collapses the registered names+aliases to one entry per
// backend (keyed by Provider.Name()).
func canonicalProviders() []string {
	seen := map[string]bool{}
	var out []string
	for _, n := range corral.RegisteredProviders() {
		p, err := corral.ProviderByName(n)
		if err != nil {
			continue
		}
		if cn := p.Name(); !seen[cn] {
			seen[cn] = true
			out = append(out, cn)
		}
	}
	return out
}

type usageResult struct {
	name string
	st   *corral.LimitStatus
	err  error
	noEP bool
}

// printUsage queries each provider concurrently and prints a block per provider,
// preserving the requested order.
func printUsage(w io.Writer, names []string) {
	results := make([]usageResult, len(names))
	var wg sync.WaitGroup
	for i, name := range names {
		p, err := corral.ProviderByName(name)
		if err != nil {
			results[i] = usageResult{name: name, err: err}
			continue
		}
		ur, ok := p.(corral.UsageReporter)
		if !ok {
			results[i] = usageResult{name: p.Name(), noEP: true}
			continue
		}
		i, ur, rn := i, ur, p.Name()
		wg.Add(1)
		go func() {
			defer wg.Done()
			st, err := ur.Usage()
			results[i] = usageResult{name: rn, st: st, err: err}
		}()
	}
	wg.Wait()

	for _, r := range results {
		switch {
		case r.err != nil:
			fmt.Fprintf(w, "%-8s  ! %v\n", r.name, r.err)
		case r.noEP:
			fmt.Fprintf(w, "%-8s  - no queryable usage endpoint\n", r.name)
		case r.st == nil:
			fmt.Fprintf(w, "%-8s  - no data (not logged in?)\n", r.name)
		default:
			plan := r.st.Plan
			if plan == "" {
				plan = "?"
			}
			fmt.Fprintf(w, "%s  (plan %s)\n", r.name, plan)
			for _, win := range r.st.Windows {
				reset := ""
				if !win.ResetsAt.IsZero() {
					reset = "  resets " + win.ResetsAt.Local().Format("Mon 02 Jan 15:04")
				}
				fmt.Fprintf(w, "   %-16s %s %5.1f%%%s\n", win.Name, bar(win.UsedPercent), win.UsedPercent, reset)
			}
		}
	}
}

// bar renders a fixed-width headroom bar for a used-percent in 0..100.
func bar(pct float64) string {
	const width = 20
	switch {
	case pct < 0:
		pct = 0
	case pct > 100:
		pct = 100
	}
	filled := int(pct/100*float64(width) + 0.5)
	return "[" + strings.Repeat("#", filled) + strings.Repeat("-", width-filled) + "]"
}
