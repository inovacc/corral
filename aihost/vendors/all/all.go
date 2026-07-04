/*
Copyright (c) 2026 inovacc
*/

// Package all blank-imports every built-in vendor so they self-register.
package all

import (
	_ "github.com/inovacc/corral/aihost/vendors/claude"
	_ "github.com/inovacc/corral/aihost/vendors/codex"
	_ "github.com/inovacc/corral/aihost/vendors/gemini"
)
