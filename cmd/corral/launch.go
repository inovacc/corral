package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/spf13/cobra"

	"github.com/inovacc/corral"
	_ "github.com/inovacc/corral/all" // register providers
)

// launchSpec describes how to locate (and, if missing, install) a provider's CLI
// for an interactive launch. The pattern is adapted from ollama's cmd/launch,
// minus the local-model routing — corral launches each agent against its own
// subscription, not a re-pointed backend.
type launchSpec struct {
	bin        string              // executable name on PATH
	fallbacks  []string            // extra absolute paths to probe if not on PATH
	install    map[string][]string // GOOS -> {bin, args...} auto-install command
	installURL string              // where to get it when auto-install isn't wired for this OS
}

func homePath(sub ...string) string {
	h, _ := os.UserHomeDir()
	return filepath.Join(append([]string{h}, sub...)...)
}

// launchSpecs is the per-provider launch registry (keyed by canonical name).
func launchSpecs() map[string]launchSpec {
	psInstall := func(url string) []string {
		return []string{"powershell", "-NoProfile", "-ExecutionPolicy", "Bypass", "-Command", "irm " + url + " | iex"}
	}
	return map[string]launchSpec{
		"claude": {
			bin:       "claude",
			fallbacks: []string{homePath(".local", "bin", "claude"), homePath(".claude", "local", "claude")},
			install: map[string][]string{
				"windows": psInstall("https://claude.ai/install.ps1"),
				"darwin":  {"bash", "-c", "curl -fsSL https://claude.ai/install.sh | bash"},
				"linux":   {"bash", "-c", "curl -fsSL https://claude.ai/install.sh | bash"},
			},
			installURL: "https://claude.ai/download",
		},
		"grok": {
			bin:        "grok",
			fallbacks:  []string{homePath(".grok", "bin", "grok")},
			install:    map[string][]string{"windows": psInstall("https://x.ai/cli/install.ps1")},
			installURL: "https://x.ai/cli",
		},
		"kimi": {
			bin:        "kimi",
			fallbacks:  []string{homePath(".kimi-code", "bin", "kimi")},
			install:    map[string][]string{"windows": psInstall("https://code.kimi.com/kimi-code/install.ps1")},
			installURL: "https://code.kimi.com",
		},
		"codex": {
			bin:        "codex",
			fallbacks:  []string{homePath("AppData", "Local", "Programs", "OpenAI", "Codex", "bin", "codex")},
			installURL: "https://developers.openai.com/codex/cli  (npm i -g @openai/codex)",
		},
		"agy": {
			bin:        "agy",
			fallbacks:  []string{homePath("AppData", "Local", "agy", "bin", "agy")},
			installURL: "https://antigravity.google",
		},
	}
}

func launchNames() []string {
	m := launchSpecs()
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// findBinary resolves the provider's CLI: PATH first, then the known fallbacks
// (adding .exe on Windows). Returns the path and whether it was found.
func (s launchSpec) findBinary() (string, bool) {
	if p, err := exec.LookPath(s.bin); err == nil {
		return p, true
	}
	for _, f := range s.fallbacks {
		cands := []string{f}
		if runtime.GOOS == "windows" && !strings.HasSuffix(strings.ToLower(f), ".exe") {
			cands = []string{f + ".exe", f}
		}
		for _, c := range cands {
			if st, err := os.Stat(c); err == nil && !st.IsDir() {
				return c, true
			}
		}
	}
	return "", false
}

// newLaunchCmd builds `corral launch <provider> [-m model] [-- args]`.
func newLaunchCmd() *cobra.Command {
	var model string
	var yes bool
	var noUsage bool
	cmd := &cobra.Command{
		Use:   "launch <provider> [-- args]",
		Short: "Launch a subscription coding-agent CLI (installing it if needed), with usage shown before/after",
		Long: "Launch one of corral's provider CLIs interactively against its OWN subscription — resolving\n" +
			"(and, if missing, installing) the binary, showing your usage before and after the session.\n" +
			"Unlike `ollama launch`, corral does NOT re-point the agent at a local model; it runs native.\n" +
			"Examples: `corral launch claude`, `corral launch agy`, `corral launch grok -- --help`.",
		Args:          cobra.MinimumNArgs(1),
		SilenceErrors: false,
		RunE: func(cmd *cobra.Command, args []string) error {
			// Resolve the provider (handles aliases like xai->grok) to a canonical name.
			p, err := corral.ProviderByName(args[0])
			if err != nil {
				return fmt.Errorf("unknown provider %q for launch (known: %s)", args[0], strings.Join(launchNames(), ", "))
			}
			name := p.Name()
			spec, ok := launchSpecs()[name]
			if !ok {
				return fmt.Errorf("no launch spec for %q (known: %s)", name, strings.Join(launchNames(), ", "))
			}

			bin, found := spec.findBinary()
			if !found {
				bin, err = ensureInstalled(cmd, name, spec, yes)
				if err != nil {
					return err
				}
			}

			if !noUsage {
				cmd.PrintErrf("── %s usage (before) ─────────────\n%s\n", name, renderUsage([]string{name}))
			}

			var cargs []string
			if model != "" {
				cargs = append(cargs, "--model", model)
			}
			cargs = append(cargs, args[1:]...)

			cmd.PrintErrf("── launching %s%s ─────────────\n", name, argsHint(cargs))
			run := exec.Command(bin, cargs...)
			run.Stdin, run.Stdout, run.Stderr = os.Stdin, os.Stdout, os.Stderr
			runErr := run.Run()

			if !noUsage {
				cmd.PrintErrf("\n── %s usage (after) ─────────────\n%s\n", name, renderUsage([]string{name}))
			}
			return runErr
		},
	}
	cmd.Flags().StringVarP(&model, "model", "m", "", "model to pass to the agent (as --model)")
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "auto-confirm installation if the CLI is missing")
	cmd.Flags().BoolVar(&noUsage, "no-usage", false, "skip the before/after usage report")
	return cmd
}

func argsHint(a []string) string {
	if len(a) == 0 {
		return ""
	}
	return " " + strings.Join(a, " ")
}

// ensureInstalled runs the provider's auto-installer (after a confirm, unless
// --yes), then re-resolves the binary. When no installer is wired for this OS it
// returns a helpful "install from <url>" error instead of failing opaquely.
func ensureInstalled(cmd *cobra.Command, name string, spec launchSpec, yes bool) (string, error) {
	inst, ok := spec.install[runtime.GOOS]
	if !ok {
		return "", fmt.Errorf("%s CLI not found. Install it from %s, then re-run `corral launch %s`", name, spec.installURL, name)
	}
	if !yes {
		cmd.PrintErrf("%s CLI is not installed. Install it now? [y/N] ", name)
		var resp string
		_, _ = fmt.Fscanln(os.Stdin, &resp)
		if !strings.HasPrefix(strings.ToLower(strings.TrimSpace(resp)), "y") {
			return "", fmt.Errorf("installation declined — get %s from %s", name, spec.installURL)
		}
	}
	cmd.PrintErrf("installing %s...\n", name)
	ic := exec.Command(inst[0], inst[1:]...)
	ic.Stdin, ic.Stdout, ic.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := ic.Run(); err != nil {
		return "", fmt.Errorf("install %s: %w", name, err)
	}
	bin, found := spec.findBinary()
	if !found {
		return "", fmt.Errorf("%s installed but not yet on PATH — restart your shell and re-run `corral launch %s`", name, name)
	}
	return bin, nil
}
