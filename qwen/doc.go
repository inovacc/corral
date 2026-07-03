// Package qwen is the Alibaba Qwen Code provider for the corral agent runtime:
// a headless `qwen --prompt <prompt>` preset. Qwen Code is a gemini-cli fork;
// its non-interactive mode needs an auth type configured
// (`--auth-type`/settings.json) and also reads the prompt from stdin, so
// oversized prompts route to stdin. Schemas are embedded in the prompt and the
// JSON is parsed from stdout.
//
// Install the backend CLI:
//
//	npm install -g @qwen-code/qwen-code
//
// (The standalone installer install-qwen-standalone.ps1 failed a checksum on
// this host; npm is the reliable path.) Repo: https://github.com/QwenLM/qwen-code.
// Verified against qwen 0.19.6.
package qwen
