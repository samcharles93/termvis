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

`termvis` runs any terminal command inside a real PTY rendered by `xterm.js` in a headless browser. It exposes that session over JSONL, so you can send keystrokes, pull back screenshots, and record the whole thing as a GIF if you want.

Common things to use it for:

- **Generate a demo GIF for a README:** script the happy path of a CLI or TUI and produce `demo.gif` with realistic typing, ready to embed in docs or release notes.
- **Document a workflow** by recording a step-by-step tutorial as a GIF, or a sequence of PNGs, with no screen-recording software involved.
- **Drive a TUI visually** so a vision-capable agent can see and operate `htop`, `vim`, `fzf`, `lazygit`, installers, and REPLs.
- Capture before/after screenshots of a CLI's output and diff them for regressions.
- Feed scripted answers to a wizard and check the final state to smoke-test interactive prompts.

## Installation

```bash
go install github.com/samcharles93/termvis@latest
```

Requires `ttyd` and a Chrome/Chromium-based browser on the host. Run `scripts/check-deps.sh` (bundled with this skill) to check for both before debugging a confusing startup failure.

Once the binary is installed, `termvis skill install` installs *this skill* to `~/.agents/skills/termvis`. The skill only exists in the git checkout otherwise; `go install` doesn't fetch it on its own. It's embedded in the binary, so this works no matter how you got `termvis` (go install, a release tarball, a local build). Flags: `-project` for `./.agents/skills/termvis`, `-dir <root>` to target another harness's skills root (`-dir ~/.claude/skills` installs to `~/.claude/skills/termvis`), `-dest <path>` for anything custom. `termvis skill show` prints it without installing.

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

A missing field means it wasn't requested, not that it's empty. Don't default-value fields you didn't ask for.

## Best Practices

- **Stability:** if you don't know how long a repaint takes, use `wait_for` instead of guessing a `wait`. It polls the text buffer (cheap, exact) and self-corrects for slow renders. Reach for a fixed `wait` only when you need a deterministic frame duration, like GIF recording.
- **Verification:** `snapshot: true` covers visual, layout, and color checks. `text: true` covers pure text assertions, and it's cheaper since there's no OCR or vision call involved.
- **Direct-to-file snapshots:** `"save": "path.png"` writes straight to disk. Skip decoding the base64 `image` field by hand unless you actually need the bytes inline.
- Batch keys with `"repeat": 5` rather than five separate JSON lines.
- **Frame timing:** GIF frame duration comes from `wait`, not `wait_for` (which has none). `"300ms"` to `"800ms"` reads well on playback.
- Set `typing_delay` (`"60ms"` works well) for screencasts, and pair it with `-i 80ms` so typing shows up as continuous frames instead of one pasted block.
- **Cleanup** happens automatically on exit. Send `ctrl`+`c` or just exit the shell.

## Vision Analysis

When analyzing the `image` returned in the response (only relevant when an agent is interactively driving the session, not for fire-and-forget GIF recording):
1. **Decode:** The `image` is a Base64-encoded PNG.
2. **Layout:** The image is 1:1 mapped to the pixel dimensions you specified at launch.
3. **Attributes:** Look for color changes (background highlights) to identify the currently focused element or selected menu item.

## Gotchas

- **`wait_for.text` matches the command line as you type it, not just its output.** The terminal echoes typed input before you even press enter. Wait for `"done"` after typing `echo done` and it can match instantly, off the input line, before the command runs. Pick a sentinel the program computes or transforms, not one you typed verbatim.
- **`wait_for.stable` doesn't suit continuously-live output.** It's built for output that settles: a command finishing, a redraw completing. A spinner or a sub-second counter may never go quiet within your timeout. Target a specific label with `wait_for.text`, or fall back to a fixed `wait`.
- **"Error starting ttyd" or "Error launching browser" almost always means a missing dependency, not a bug.** Run [`scripts/check-deps.sh`](scripts/check-deps.sh) first.

## Recording & Playback

- **Record:** Add `-o session.gif` to save the run. Combine with `-i 80ms` to capture continuously while keystrokes happen, so the resulting GIF shows typing motion rather than only before/after states.
- **Play back:** `termvis -v session.gif` renders an existing GIF inline using the Kitty graphics protocol (Ctrl+C to stop). Useful for previewing a recording without leaving the terminal.

## Worked Examples

See [`references/examples.md`](references/examples.md) for canonical JSONL recipes: generating a README demo GIF, navigating a menu-driven TUI, smoke-testing a CLI, live observation, and recovering from unexpected UI states.

## MCP Server

`termvis mcp` runs the same capability as an MCP server: `open_session`, `send_action`, `close_session`, and `list_sessions` tools (`send_action`'s `action` argument is the same JSON schema above). Each `open_session` call re-execs the `termvis` binary to drive one session, so there's no separate binary or PATH setup. Lost track of a `session_id` after a context gap or a crashed task? Call `list_sessions` to recover or close it instead of leaking the worker process.

- **Stdio (default):** `termvis mcp`, for clients that spawn the process directly (Claude Code, Claude Desktop).
- **HTTP/SSE:** `termvis mcp -http :8080` runs the [Streamable HTTP transport](https://modelcontextprotocol.io/2025/03/26/streamable-http-transport.html), so termvis can run as a standalone service instead of being spawned per client.

**Prefer registering it as an MCP server** over shelling out via Bash and hand-rolled JSONL when your harness supports MCP. You get structured tool calls and native image content, and skip constructing and parsing JSON by hand. Generic client config:

```json
{
  "mcpServers": {
    "termvis": { "command": "termvis", "args": ["mcp"] }
  }
}
```

For Claude Code specifically: `claude mcp add termvis -- termvis mcp`.

**Security:** `open_session` runs arbitrary shell commands, and the HTTP transport has no built-in authentication. Never bind `-http` to a public interface without your own auth (reverse proxy, mTLS) or network isolation, like a sandboxed container with no inbound access from untrusted networks. An exposed `-http` endpoint is unauthenticated remote code execution.

See [`references/examples.md`](references/examples.md) for worked MCP tool-call recipes.
