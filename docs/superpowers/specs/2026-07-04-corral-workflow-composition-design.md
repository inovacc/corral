# corral Workflow Composition (adk-go patterns, execution plane) — Design

**Status:** Approved to plan · 2026-07-04 · module `github.com/inovacc/corral`

## 0. Problem

corral runs exactly **one agent per call** (`Agency.RunAgent(ctx, Agent, input) (RunResult, error)`). It has no way to compose multiple agent turns — no pipeline, fan-out, or refinement loop. Google's adk-go proves a clean vocabulary for this (`sequentialagent` / `parallelagent` / `loopagent`). This ports the *shape* of that composition — **not adk-go as a dependency** (adk's `model.LLM` is per-token genai API, which inverts corral's subscription-CLI thesis and its one-dep budget). This is the **execution plane** of "corral as agent control plane"; the asset/definition plane is a separate, parallel design.

## 1. Scope (adk-go pattern port — Slice 1 of 4)

**In:** sequential / parallel / loop composition of agent turns, built on corral's existing `Agency.RunAgent`. A new `workflow.go` (package `corral`), decoupled from `Agency` via a small runner seam so it is testable with a fake (no real provider / CLI).

**Deferred (later slices, explicit non-goals here):** session-state scoping (`app:`/`temp:` prefixes) — Slice 2; run event streams (`Event`/`EventActions`, an optional `RunStreamer` + `EventSink`) — Slice 3; tool-filtering (`Toolset`/`FilterToolset` over `Agent.Tools`) — Slice 4.

## 2. Design

### 2.1 The runner seam (`workflow.go`, package `corral`)
```go
// AgentRunner is the single-turn seam that workflows drive. *Agency satisfies
// it (via its RunAgent method), so workflows compose real agent turns; tests
// supply a fake runner and need no provider/CLI.
type AgentRunner interface {
	RunAgent(ctx context.Context, ag Agent, input string) (RunResult, error)
}
```
Decoupling from `*Agency` (rather than adding methods to it) keeps `Agency` focused on the single-turn contract and makes composition unit-testable without a live backend. `*Agency` already has the exact method, so it satisfies `AgentRunner` with no change.

### 2.2 Step
```go
// Step is one agent turn in a workflow. Input derives this step's input from
// the previous result; nil means "use the previous step's RunResult.Text"
// (for the first step, the workflow seed is used).
type Step struct {
	Agent Agent
	Input func(prev RunResult) string
}
```
Because `RunAgent` takes an **explicit `Agent`** (not just a roster name), composed/synthetic agents work without registration.

### 2.3 The three primitives
```go
// Sequential runs steps in order, threading each RunResult.Text into the next
// step's input (unless that step overrides via Step.Input). Stops on the first
// error, returning the results gathered so far plus the error.
func Sequential(ctx context.Context, r AgentRunner, seed string, steps ...Step) ([]RunResult, error)

// Parallel runs every step concurrently against the same input, returning
// results in step order. Returns the first error encountered (all goroutines
// still complete). See the warm-session caveat below.
func Parallel(ctx context.Context, r AgentRunner, input string, steps ...Step) ([]RunResult, error)

// Loop runs step repeatedly, feeding each RunResult back as the next input,
// until until(result) reports done or maxIters is reached (maxIters <= 0 is an
// error — an unbounded loop is never allowed). Returns every iteration's result.
func Loop(ctx context.Context, r AgentRunner, seed string, step Step, until func(RunResult) bool, maxIters int) ([]RunResult, error)
```

## 3. Semantics & error handling
- **Sequential:** step *i*'s input = `Step.Input(prev)` if set, else `prev.Text` (seed for step 0). First error aborts; returns partial results + wrapped error (`fmt.Errorf("workflow: step %d (%s): %w", …)`).
- **Parallel:** all steps get the same `input`; run in goroutines; `context` cancellation respected; results collected in step order; the first non-nil error is returned (every goroutine still finishes so nothing leaks). **Caveat (documented):** `SessionPool` keys one warm session per `agent.Name`, so two parallel steps with the *same* agent name contend on one session — parallel branches SHOULD use distinct agent names; same-name branches serialize (correct, just slower).
- **Loop:** `until` is checked after each run; `maxIters <= 0` → immediate error; returns all iteration results (so the caller can inspect the trajectory), plus an error if the cap was hit without `until` succeeding (a sentinel `ErrLoopExhausted`).
- All three respect `ctx` (checked between steps/iterations and passed into `RunAgent`).

## 4. Constraints honored
- **No new dependency** — pure stdlib (`context`, `fmt`, `sync`); adk-go is NOT imported. corral keeps its one-external-dep (`conpty`) budget.
- **Subscription-CLI thesis intact** — composition sits *above* `RunAgent`; each turn is still a subscription-CLI run bounded by the plan, not a metered API call.
- **corral idioms** — mirrors the codebase's functional style and the `AgentRunner`-seam pattern (like the existing `SessionOpener`/`UsageReporter` optional-capability seams); tests table-style with a fake runner.

## 5. Testing (network-free, fake runner)
- `fakeRunner` records calls and returns scripted `RunResult`s (or errors).
- **Sequential:** asserts order, that each step receives the prior `Text` (and that `Step.Input` overrides work), and that a mid-pipeline error aborts with partials returned.
- **Parallel:** asserts all steps ran on the same input, results are in step order, and a failing branch surfaces its error while others still complete.
- **Loop:** asserts it re-feeds output, stops when `until` is satisfied, and returns `ErrLoopExhausted` at `maxIters`; `maxIters<=0` errors immediately.
- A runnable `Example` (godoc) composing two fake steps.

## 6. Non-goals
- No adk-go dependency; no `model.LLM` bridge; no genai types.
- No session-state, event-stream, or tool-filtering here (Slices 2-4).
- No change to `Agency`, `Provider`, `RunRequest`, or `RunResult` (composition is purely additive).
