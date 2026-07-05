---
name: termvis
description: >
  Use this skill when the user wants to programmatically drive a real terminal
  and capture what it shows — as a screenshot, an in-terminal live view, or a
  recorded GIF. Trigger for asks like "record a demo GIF of my CLI for the
  README," "make a screencast of this TUI," "animate my installer for the
  docs," or "screenshot my command's output." Also trigger when an agent needs
  to drive an interactive TUI itself (htop, vim, fzf, lazygit, REPLs,
  installers, wizards) and visually verify state. Apply even when the user
  describes the goal without naming the domain — e.g., "create a visual for
  the README," "show how my tool looks in action," or "walk through this
  prompt automatically" — and for mixed-intent requests where recording or
  visual verification is one step among many.
---

> **Source of truth:** the canonical agent instructions live in [`AGENTS.md`](../../AGENTS.md) at the repository root. This `SKILL.md` is a duplicate maintained for the skill packaging format. If you edit one, mirror the change in the other (this will be automated later).

# Using `termvis`

`termvis` runs any terminal command inside a real PTY rendered by `xterm.js` in a headless browser, then exposes it over JSONL: send keystrokes, receive screenshots, optionally record the whole session as a GIF.

Common things to use it for:

- **Generate a demo GIF for a README** — script the typical happy path of a CLI/TUI and produce `demo.gif` with realistic typing, suitable for embedding in documentation or release notes.
- **Document a workflow** — record a step-by-step tutorial as a GIF (or a sequence of PNGs) without needing manual screen-recording software.
- **Visually drive a TUI** — give a vision-capable agent the ability to see and operate `htop`, `vim`, `fzf`, `lazygit`, installers, REPLs, etc.
- **Regression-check terminal UI** — capture before/after screenshots of a CLI's output and diff them.
- **Smoke-test interactive prompts** — feed scripted answers to a wizard and verify the final state.

## Installation

```bash
go install github.com/samcharles93/termvis@latest
```

Requires `ttyd` and a Chrome/Chromium-based browser on the host — run `scripts/check-deps.sh` (bundled with this skill) to check for both before debugging a confusing startup failure.

Once the binary is installed, `termvis skill install` installs *this skill* to `~/.agents/skills/termvis` — the skill only exists in the git checkout otherwise, since it's not something `go install` fetches on its own. It's embedded in the binary, so this works regardless of how you got `termvis` (go install, a release tarball, a local build). Flags: `-project` for `./.agents/skills/termvis`, `-dir <root>` to target another harness's skills root (e.g. `-dir ~/.claude/skills` installs to `~/.claude/skills/termvis`), `-dest <path>` for a fully custom path. `termvis skill show` prints it without installing.

## Execution Model

1. **Launch:** Run `termvis [flags] -- [command]` (e.g. `termvis -- htop`, or `termvis -o demo.gif -- bash` to record a shell session).
2. **REPL Loop:**
   - **Send Action:** Output a JSON line to `stdin`.
   - **Receive State:** Read a JSON response from `stdout`.
   - **Observe (optional):** If you're driving the session interactively, decode the `image` field (Base64 PNG) and/or read the `text` field (plain-text buffer dump) to inspect terminal state. If you're just generating a GIF you can ignore responses.

## Command Schema (JSON)

Every command is a single-line JSON object:

```json
{
  "action": "string",        // "type", "key", "ctrl", "enter"
  "value": "string",         // The text to type or key name (e.g., "down", "C")
  "repeat": 1,               // Optional: repeat the action N times
  "snapshot": true,          // Optional: request an image snapshot of the result
  "save": "path.png",        // Optional: also/instead write the PNG snapshot directly to this path
  "text": true,               // Optional: request a plain-text dump of the visible buffer
  "wait": "200ms",           // Optional: fixed pause for UI stability before snapshot
  "wait_for": {              // Optional: poll instead of sleeping a fixed duration (overrides "wait")
    "text": "Done",           //   wait until the buffer contains this substring
    "stable": true,           //   and/or wait until the buffer stops changing
    "timeout": "3s"           //   give up after this long (default 2s); does not error, see below
  },
  "typing_delay": "40ms"     // Optional: per-keystroke delay (overrides --type-delay)
}
```

### Supported Actions & Values

- **`type`**: Types the string in `value`. Handles Shift/Symbols automatically.
- **`key`**: Presses a special key.
  - *Values:* `up`, `down`, `left`, `right`, `enter`, `backspace`, `tab`, `escape`, `space`.
- **`ctrl`**: Sends Ctrl + key.
  - *Values:* `a`-`z`, `C`, etc.
- **`enter`**: Shortcut for `{"action": "key", "value": "enter"}`.

## Response Fields

```json
{
  "status": "success",       // "success" or "error"
  "image": "...",            // Present only if "snapshot": true was requested — omitted entirely otherwise, not an empty string
  "text": "...",             // Present only if "text": true was requested
  "saved_to": "path.png",    // Present only if "save" was requested and the write succeeded
  "timed_out": true,         // Present (and true) only if a "wait_for" condition never became true before its timeout
  "error": "..."             // Present only when status is "error"
}
```

A field's absence means "not requested," not "empty" — don't defensively default-value fields you didn't ask for.

## Best Practices

- **Stability:** Prefer `wait_for` (`text` or `stable`) over a fixed `wait` when you're not sure how long a repaint takes — it polls the terminal's text buffer (cheap, exact) instead of guessing a duration, and self-corrects for slow renders instead of you padding every wait defensively. Use a fixed `wait` when you specifically need a deterministic frame duration (e.g. GIF recording).
- **Verification:** Request `snapshot: true` (for visual/layout/color checks) and/or `text: true` (for pure text-content assertions) whenever you need to confirm the UI has transitioned to the expected state. `text` is exact (no OCR, no vision call needed) and is the cheaper choice whenever color/layout isn't the thing being tested.
- **Direct-to-file snapshots:** Use `"save": "path.png"` to write a snapshot straight to disk instead of decoding the base64 `image` field yourself — useful for anything beyond a single quick inline check.
- **Batched Keys:** Use `"repeat": 5` to navigate menus or lists quickly instead of sending 5 separate JSON lines.
- **Frame timing:** When a snapshot is recorded to GIF, its frame duration is taken from `wait` (not `wait_for`, which has no fixed duration). Use realistic values (e.g. `"300ms"`–`"800ms"`) to keep playback readable.
- **Visible typing:** For screencasts and demo GIFs, set `typing_delay` (e.g. `"60ms"`) so individual keystrokes are observable rather than appearing as a single paste. Combine with `-i 80ms` to capture continuous frames during typing.
- **Cleanup:** The tool handles PTY cleanup automatically on exit. To exit, send a `ctrl` + `c` action or exit the shell.

## Vision Analysis

When analyzing the `image` returned in the response (only relevant when an agent is interactively driving the session, not for fire-and-forget GIF recording):
1. **Decode:** The `image` is a Base64-encoded PNG.
2. **Layout:** The image is 1:1 mapped to the pixel dimensions you specified at launch.
3. **Attributes:** Look for color changes (background highlights) to identify the currently focused element or selected menu item.

## Gotchas

- **`wait_for.text` matches the command line as you type it, not just its output.** The terminal echoes typed input immediately, before you press enter — if your sentinel string also appears in the command itself (e.g. waiting for `"done"` after typing `echo done`), it can match instantly, before the command has even run. Pick a sentinel that can't appear in the input line (e.g. a value the program computes or transforms, not one you typed verbatim).
- **`wait_for.stable` doesn't suit continuously-live output.** It's for output that settles (a command finishing, a menu redraw completing) — a display that keeps changing on a fast cadence (an animated spinner, a sub-second live counter) may never go quiet within your timeout. Use a fixed `wait` or `wait_for.text` targeting a specific label instead.
- **A confusing "Error starting ttyd" / "Error launching browser" almost always means a missing dependency, not a bug.** Run [`scripts/check-deps.sh`](scripts/check-deps.sh) to check for `ttyd` and a Chrome/Chromium browser before debugging further.

## Recording & Playback

- **Record:** Add `-o session.gif` to save the run. Combine with `-i 80ms` to capture continuously while keystrokes happen, so the resulting GIF shows typing motion rather than only before/after states.
- **Play back:** `termvis -v session.gif` renders an existing GIF inline using the Kitty graphics protocol (Ctrl+C to stop). Useful for previewing a recording without leaving the terminal.

## Worked Examples

See [`references/examples.md`](references/examples.md) for canonical JSONL recipes — generating a README demo GIF, navigating a menu-driven TUI, smoke-testing a CLI, live observation, and recovering from unexpected UI states.

## MCP Server

`termvis mcp` runs the same capability as an MCP server, exposing `open_session`, `send_action`, `close_session`, and `list_sessions` tools (the `action` argument to `send_action` is the same JSON schema described above). Each `open_session` call re-execs the `termvis` binary itself to drive one session — no separate binary or PATH setup needed. If you lose track of a `session_id` (context compaction, a crashed task), call `list_sessions` to recover it or close it rather than leaking the worker process.

- **Stdio (default):** `termvis mcp` — for MCP clients that spawn the process directly (Claude Code, Claude Desktop, etc.).
- **HTTP/SSE:** `termvis mcp -http :8080` — runs the [Streamable HTTP transport](https://modelcontextprotocol.io/2025/03/26/streamable-http-transport.html) (SSE-based) so termvis can run as a standalone service on your own infrastructure instead of being spawned per-client.

**Prefer registering it as an MCP server** over shelling out via Bash and hand-rolled JSONL when your harness supports MCP — you get structured tool calls and native image content instead of manually constructing and parsing JSON. Generic client config:

```json
{
  "mcpServers": {
    "termvis": { "command": "termvis", "args": ["mcp"] }
  }
}
```

For Claude Code specifically: `claude mcp add termvis -- termvis mcp`.

**Security:** `open_session` runs arbitrary shell commands. The HTTP transport has no built-in authentication — never bind `-http` to a public interface without your own auth (reverse proxy, mTLS) or network isolation (e.g. a sandboxed container with no inbound access from untrusted networks). Treat an exposed `-http` endpoint as unauthenticated remote code execution.

See [`references/examples.md`](references/examples.md) for worked MCP tool-call recipes.
