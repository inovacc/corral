// Package all blank-imports every provider package so their init() registers
// the providers with corral. Import it (with _) wherever ProviderByName is used
// so agy/claude/codex/grok/kimi/qwen are available.
package all

import (
	_ "github.com/inovacc/corral/agy"
	_ "github.com/inovacc/corral/claude"
	_ "github.com/inovacc/corral/codex"
	_ "github.com/inovacc/corral/grok"
	_ "github.com/inovacc/corral/kimi"
	_ "github.com/inovacc/corral/qwen"
)
