// Package all blank-imports every provider package so their init() registers
// the providers with pkg/agents. Import it (with _) wherever ProviderByName is
// used so codex/claude/agy are available.
package all

import (
	_ "github.com/inovacc/agents/agy"
	_ "github.com/inovacc/agents/claude"
	_ "github.com/inovacc/agents/codex"
)
