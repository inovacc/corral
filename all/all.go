// Package all blank-imports every provider package so their init() registers
// the providers with corral. Import it (with _) wherever ProviderByName is
// used so codex/claude/agy are available.
package all

import (
	_ "github.com/inovacc/corral/agy"
	_ "github.com/inovacc/corral/claude"
	_ "github.com/inovacc/corral/codex"
)
