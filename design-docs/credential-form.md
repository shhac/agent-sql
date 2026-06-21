# Credential Form (`credential add --form`) Design Document

## Problem

Non-technical users wanting to give an LLM the ability to query a database often paste secrets directly into the LLM ("here is my password, please configure agent-sql"). The LLM then sees the secret in its context window — it may be logged, stored in transcripts, used to train models, or surface in completion telemetry. We want a path where the LLM can drive credential setup *without ever seeing the secret*.

## Constraints

- Must work in **Claude Code in Terminal**, **Claude Desktop**, **Codex Desktop**, and any other LLM agent shelling out to `agent-sql`.
- Must fail loudly (not hang) when no GUI is available — e.g. SSH sessions, headless CI, container builds.
- Must preserve agent-sql's "single static binary, no CGo" invariant.
- LLM-visible stdout must contain only a redacted receipt — never the secret value.
- Must not break existing scripted/CI usage of `credential add --username … --password …`.

## Options considered

### A. TTY prompt (`/dev/tty` + `term.ReadPassword`)

Open `/dev/tty` directly, read with terminal echo off. Bypasses the LLM-driven stdin pipe.

**Rejected.** Only works if a human types `agent-sql credential add --form` directly into their own shell — not when an LLM agent is the foreground process. The LLM holds the controlling terminal in Claude Code; the spawned subprocess either gets SIGTTIN'd or blocks indefinitely. Doesn't work at all in Claude Desktop or Codex Desktop where there is no terminal.

### B. Localhost browser form

Bind `127.0.0.1` on a random port, mint a one-time token, open the user's default browser to a single-page form, block until POST callback.

**Viable but heavier.** Ship a tiny HTTP server, embed HTML, request a port, manage the token. Pattern used by `gh auth login`, `gcloud auth login`. Universal — works in every host environment. ~150 lines of Go plus an HTML asset.

**Deferred.** Heavier than option C and adds attack surface (local port, even if loopback). Reasonable v2 if option C proves insufficient.

### C. Native OS dialog

Pop a native dialog on the user's screen via the OS's built-in mechanism: `osascript` on macOS, `zenity`/`kdialog` on Linux, Win32 API on Windows.

**Chosen.** Smallest implementation, no port binding, no HTML, and matches the user's mental model of "the tool needs my password — it asked me with a popup."

## Library choice: `ncruces/zenity`

Rather than write three platform implementations ourselves, we use [`github.com/ncruces/zenity`](https://github.com/ncruces/zenity).

Properties that match our constraints:

- **Pure Go, no CGo.** Preserves single-binary, cross-compilable build.
- **Windows: direct Win32 API calls** via `syscall` — no shell-out to PowerShell, no dependence on the user's shell choice (cmd vs pwsh vs Windows Terminal — all irrelevant from a Go binary).
- **macOS: `osascript`** — exactly what we'd write.
- **Linux: probes `zenity` → `qarma` → `matedialog`** in that order — frees us from writing detection.
- **Has `Entry` (text) and `Password` (hidden) primitives** that map 1:1 to our `InputType` enum.
- **Active.** `gen2brain/dlgs` (the obvious alternative) is archived and explicitly recommends this as its successor. `sqweek/dialog` was rejected — uses CGo on Windows.

What it *doesn't* have: a multi-field form primitive. Our `Spec.Items` shape naturally maps to "loop over items, call `Entry` or `Password` for each, collect results." This becomes the chained-popup pattern on every platform — fine for our use case because in practice only one or two fields are ever requested (see "Why chaining is fine" below).

## Binary size impact

Measured on `darwin/arm64`, `-ldflags "-s -w"`:

| | Bytes | Size |
|---|---|---|
| Baseline (no zenity) | 16,394,962 | 15.6 MiB |
| With zenity wired into `credential --form` | 19,105,938 | 18.2 MiB |
| **Delta** | **+2,710,976** | **+2.6 MiB (~16.5%)** |

(An earlier first-pass measurement of "+150 KiB" referenced only a single zenity symbol; Go's linker dead-code-eliminated most of the package. With the real implementation calling `Password`, `Entry`, `Title`, and `ErrCanceled`, more of the package stays reachable.)

Acceptable for a CLI tool — agent-sql is still a single static binary at 18 MiB, comparable to `gh`, `gcloud` components, and other typical Go CLIs. The cost buys cross-platform native dialogs with no CGo and no per-platform code we own.

If size becomes a concern in future, options to revisit:
- Build a tag-gated `-tags=noform` build that excludes zenity for size-sensitive distributions.
- Inline the small subset of zenity we use (Password + Entry on three platforms ≈ 600 LOC) and shed the 2.6 MiB of unused dialog types.

## Repo strategy

**In-tree first; copy to siblings when they need it; extract only at project #3.**

The wrapper around zenity is ~110 lines including headless detection. Extracting that to a shared module costs:

- A new repo to maintain (CI, releases, versioning).
- N agent-sql-style sibling projects all importing it and drifting on version.
- Premature abstraction calcifying the wrong shape before we have multiple real callers.

When the second sibling project needs this, we copy the package. The cost of duplication is one pull-request worth of files. When a third sibling needs it — at which point we have actual evidence about what generalizes and what doesn't — we extract. The shared module's value-add at that point is the LLM-shaped error envelope and `Available()` headless detection, *not* the dialog rendering (zenity already covers that).

## Interface

```go
package dialog

type InputType int
const (
    Text InputType = iota
    Password
)

type Field struct {
    ID        string
    Label     string
    InputType InputType
}

type Spec struct {
    Title string
    Items []Field
}

type Result struct {
    ID    string
    Value string
}

type Prompter interface {
    Prompt(ctx context.Context, spec Spec) ([]Result, error)
    Available() error
}

// Default is the package-level Prompter used by callers.
// Tests can swap it via SetDefault(fake) and restore in t.Cleanup.
var Default Prompter = &zenityPrompter{}

func SetDefault(p Prompter) (restore func())
```

## Errors

```go
var (
    ErrCancelled   = errors.New("cancelled by user")
    ErrNoGUI       = errors.New("no GUI dialog available")
    ErrUnsupported = errors.New("platform unsupported")
)
```

Errors are wrapped with field-level detail (`"%w: at step %d of %d (%s)"`). The CLI converts them into `errors.QueryError` envelopes:

| Sentinel | `fixable_by` | Hint |
|---|---|---|
| `ErrCancelled` | `retry` | "User cancelled the dialog. Re-run `agent-sql credential add --form` to retry." |
| `ErrNoGUI` | `human` | "agent-sql credential add --form requires a graphical desktop session. Ask the user to run it on their local machine, or fall back to: agent-sql credential add \<name\> --username \<u\> --password \<secret\>" |
| `ErrUnsupported` | `human` | "This platform does not support GUI dialogs." |

## Headless detection

zenity itself fails *eventually* in headless contexts, but the errors are platform-specific and not ideal for an LLM. We pre-flight in `Available()`:

| Platform | Check |
|---|---|
| **darwin** | If `$SSH_CONNECTION` set AND `$TERM_PROGRAM` empty → `ErrNoGUI` (likely SSH'd in with no GUI session). Otherwise allow; let osascript surface its own error if the GUI session has been torn down. |
| **linux** | `$DISPLAY` or `$WAYLAND_DISPLAY` must be set, AND one of `zenity`/`kdialog` must be on PATH. |
| **windows** | `$SESSIONNAME` must be set (`Console` or `RDP-Tcp#N`). Win32-OpenSSH leaves it unset; service contexts also fail. |

These are best-effort — they catch the common failures cleanly and let the OS surface anything else.

## Why chaining popups is fine

zenity has no multi-field form. Each `Field` becomes one popup. In practice this is almost always **one** popup:

| Driver | Username | Password | Other | Popups when `--form` used |
|---|---|---|---|---|
| Postgres / Cockroach / MySQL / MariaDB / MSSQL | `--username <u>` | needs prompt | — | **1** (password) |
| Snowflake | n/a | PAT — needs prompt | — | **1** (PAT) |
| SQLite | n/a | n/a | — | **0** (no-op) |
| Power-user case (omit `--username` to keep it out of shell history) | needs prompt | needs prompt | — | **2** |

When ≥2 popups are needed, the title bar shows `<spec.Title> (step N of M)` so the user sees the progress.

## Cancellation

Cancelling at popup N of M:

1. Aborts the chain immediately — subsequent popups are not shown.
2. Returns `ErrCancelled` wrapped with the step number and field label.
3. CLI emits the receipt-shaped error envelope and exits non-zero.
4. The LLM sees `{"error":"cancelled by user: at step 1 of 2 (Database password)","fixable_by":"retry","hint":"…"}`.

## DI and tests

Tests must be fully automated; running `go test ./...` cannot pop dialogs.

The `Default` package-level var is what callers use. Tests swap it:

```go
restore := dialog.SetDefault(&fakePrompter{    promptFunc: func(ctx context.Context, spec Spec) ([]Result, error) {        return []Result{{ID: "password", Value: "test-pw"}}, nil    },})
defer restore()
```

A small `internal/dialog/fake.go` (or table helpers in the test file) provides the standard fake. Real-popup tests live in `*_manual_test.go` behind a `//go:build manual` constraint, opt-in via `go test -tags=manual ./internal/dialog/...`.

## CLI semantics

`agent-sql credential add <name> --form`:

1. Parse `--username`, `--password`, `--write` as today.
2. If `--form` is set:
   - Build `Spec` containing only fields that are missing (skip ones supplied by flags).
   - Call `dialog.Default.Available()`. If it returns an error, emit envelope and exit non-zero.
   - Call `dialog.Default.Prompt(ctx, spec)`. On `ErrCancelled`, emit envelope and exit non-zero.
   - Fold results back into the `credential.Credential` struct.
3. Continue with the existing `credential.Store(name, cred)` flow.

`--form` is **opt-in**. Existing scripted callers pass `--password` and don't see any new behavior.

The `--form` flag does not refuse `--password` when both are passed — that policy is enforced by SKILL.md telling the LLM never to accept pasted secrets, not by the CLI mechanism. Keeping the CLI orthogonal makes it easier to explain and test.

## SKILL.md update

Adding a load-bearing rule:

> **Never accept pasted secrets.** If the user supplies a database password or PAT in chat, do not put it in the `--password` flag. Instead, instruct the user to run `agent-sql credential add <name> [--username <u>] --write|--no-write --form` themselves — a native dialog will pop up on their screen and they can type the secret directly into the OS. The CLI returns only a redacted JSON receipt; the secret never enters the LLM context.

This is the load-bearing piece. The CLI affordance only helps if the skill steers LLMs toward it.

## Files touched

**New:**
- `internal/dialog/dialog.go` — interface, types, errors, `Default`, `SetDefault`
- `internal/dialog/zenity.go` — `zenityPrompter` implementation
- `internal/dialog/available_darwin.go`
- `internal/dialog/available_linux.go`
- `internal/dialog/available_windows.go`
- `internal/dialog/dialog_test.go` — automated tests with fake `Prompter`
- `internal/dialog/zenity_manual_test.go` — `//go:build manual` real-popup test

**Modified:**
- `go.mod` / `go.sum` — `+ github.com/ncruces/zenity`
- `internal/cli/credential/credential.go` — `--form` flag wiring
- `internal/cli/credential/credential.go` — usage text gains `--form` section
- `internal/cli/usage.go` — top-level reference card mentions `--form`
- `skills/agent-sql/SKILL.md` — "never accept pasted secrets" rule
- `skills/agent-sql/references/commands.md` — `--form` documented
- `README.md` — "Secure credential entry" section
- `CLAUDE.md` — already lists the docs to update; no change needed

## Out of scope for v1

- Localhost browser form (option B) — left for v2 if dialog UX is insufficient.
- Storing the form result in 1Password / system keyring directly (we already use Keychain on macOS via `credential.Store`).
- Pre-filling the form from a previously-stored credential (overwrite is the only flow today).
- Custom field types beyond `Text` and `Password` (e.g. dropdown for write/read-only). `--write` stays a CLI flag — it's a policy choice the LLM should articulate explicitly, not a UI element competing for the user's attention with the secret entry.
