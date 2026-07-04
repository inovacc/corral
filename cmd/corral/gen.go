/*
Copyright (c) 2026 inovacc
*/

package main

import (
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/inovacc/corral/aihost"
	_ "github.com/inovacc/corral/aihost/vendors/all" // register claude/gemini/codex
)

// newGenCmd builds `corral gen [--config corral.json] [--out ./gen] [--vendor all] [--dry-run]`:
// it loads a component spec (aihost.Load), generates the requested vendors'
// component trees (aihost.Generate), and either previews the planned file
// paths (--dry-run) or writes them to disk (aihost.WriteTree).
func newGenCmd() *cobra.Command {
	var (
		config string
		out    string
		vendor string
		dryRun bool
	)
	const allVendors = "all"
	cmd := &cobra.Command{
		Use:   "gen",
		Short: "Generate per-vendor components from a corral.json spec",
		Long: "Load a corral.json component spec and generate, for each target vendor\n" +
			"(claude/gemini/codex), an installable plugin tree plus a Go module wrapper.\n" +
			"Pass --dry-run to preview the planned file paths without writing anything.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			c, err := aihost.Load(config)
			if err != nil {
				return err
			}

			vendors := c.Vendors
			if vendor != "" && vendor != allVendors {
				vendors = []string{vendor}
			}
			if len(vendors) == 0 {
				return fmt.Errorf("corral gen: no vendors to generate (pass --vendor or set component.vendors in %s)", config)
			}

			files, err := aihost.Generate(c, vendors)
			if err != nil {
				return err
			}

			w := cmd.OutOrStdout()
			if dryRun {
				return printGenDryRun(w, files)
			}

			n, err := aihost.WriteTree(files, out)
			if err != nil {
				return err
			}
			return printGenSummary(w, files, out, n)
		},
	}
	cmd.Flags().StringVar(&config, "config", "corral.json", "path to the component spec")
	cmd.Flags().StringVar(&out, "out", "./gen", "output directory for generated components")
	cmd.Flags().StringVar(&vendor, "vendor", allVendors, `target vendor (e.g. "claude"), or "all" for every vendor in the spec`)
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "print planned file paths without writing them")

	cmd.AddCommand(newGenListCmd())
	return cmd
}

// printGenDryRun writes each planned relative path followed by a trailing count.
func printGenDryRun(w io.Writer, files []aihost.GeneratedFile) error {
	for _, f := range files {
		if _, err := fmt.Fprintln(w, f.Path); err != nil {
			return err
		}
	}
	_, err := fmt.Fprintf(w, "%d files planned\n", len(files))
	return err
}

// printGenSummary writes a per-vendor file count followed by the total written.
func printGenSummary(w io.Writer, files []aihost.GeneratedFile, out string, n int) error {
	counts := map[string]int{}
	var vendors []string
	for _, f := range files {
		v := vendorOf(f.Path)
		if _, ok := counts[v]; !ok {
			vendors = append(vendors, v)
		}
		counts[v]++
	}
	sort.Strings(vendors)
	for _, v := range vendors {
		if _, err := fmt.Fprintf(w, "%s: %d files\n", v, counts[v]); err != nil {
			return err
		}
	}
	_, err := fmt.Fprintf(w, "%d files written to %s\n", n, out)
	return err
}

// vendorOf extracts the leading path segment (the vendor name) from a
// GeneratedFile.Path — Generate always prefixes each path as "<vendor>/...".
func vendorOf(path string) string {
	if i := strings.IndexByte(path, '/'); i >= 0 {
		return path[:i]
	}
	return path
}

// newGenListCmd builds `corral gen list [--config corral.json]`: prints the
// component spec's asset counts by kind plus its target vendors.
func newGenListCmd() *cobra.Command {
	var config string
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List a component spec's asset counts and target vendors",
		RunE: func(cmd *cobra.Command, _ []string) error {
			c, err := aihost.Load(config)
			if err != nil {
				return err
			}

			counts := map[aihost.AssetKind]int{}
			var kinds []string
			for _, a := range c.Assets {
				if _, ok := counts[a.Kind]; !ok {
					kinds = append(kinds, string(a.Kind))
				}
				counts[a.Kind]++
			}
			sort.Strings(kinds)

			w := cmd.OutOrStdout()
			for _, k := range kinds {
				if _, err := fmt.Fprintf(w, "%-10s %d\n", k, counts[aihost.AssetKind(k)]); err != nil {
					return err
				}
			}
			_, err = fmt.Fprintf(w, "vendors: %s\n", strings.Join(c.Vendors, ", "))
			return err
		},
	}
	cmd.Flags().StringVar(&config, "config", "corral.json", "path to the component spec")
	return cmd
}
