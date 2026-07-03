// Package kimi is the Moonshot AI Kimi Code provider for the corral agent
// runtime: a headless `kimi --prompt <prompt>` preset that auto-approves tools
// via --yolo so a non-interactive turn never blocks. Schemas are embedded in
// the prompt and JSON is parsed from stdout (--output-format defaults to text).
//
// Install the backend CLI (Windows):
//
//	irm https://code.kimi.com/kimi-code/install.ps1 | iex
//
// Repo: https://github.com/MoonshotAI/kimi-code. Auth via `kimi` (Moonshot
// subscription); config under ~/.kimi-code. Verified against kimi 0.22.2.
package kimi
