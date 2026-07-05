# Instructions for AI Agents: using `termvis`

`termvis` is your interface for interacting with the terminal in real-time. It allows you to "see" the terminal state as an image and send keystrokes to interact with it.

## Execution Model

1. **Launch:** Run `termvis [flags] -- [command]`.
2. **REPL Loop:**
   - **Send Action:** Output a JSON line to `stdin`.
   - **Receive State:** Read a JSON response from `stdout`.
   - **Observe:** Analyze the `image` (Base64 PNG) and/or `text` (plain-text buffer dump) to decide your next move.

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
- **Visible typing:** For agent-authored screencasts, set `typing_delay` (e.g. `"60ms"`) so individual keystrokes are observable rather than appearing as a single paste.
- **Cleanup:** The tool handles PTY cleanup automatically on exit. To exit, send a `ctrl` + `c` action or exit the shell.

## Vision Analysis

When analyzing the `image` returned in the response:
1. **Decode:** The `image` is a Base64-encoded PNG.
2. **Layout:** The image is 1:1 mapped to the pixel dimensions you specified at launch.
3. **Attributes:** Look for color changes (background highlights) to identify the currently focused element or selected menu item.

## Gotchas

- **`wait_for.text` matches the command line as you type it, not just its output.** The terminal echoes typed input immediately, before you press enter — if your sentinel string also appears in the command itself (e.g. waiting for `"done"` after typing `echo done`), it can match instantly, before the command has even run. Pick a sentinel that can't appear in the input line (e.g. a value the program computes or transforms, not one you typed verbatim).
- **`wait_for.stable` doesn't suit continuously-live output.** It's for output that settles (a command finishing, a menu redraw completing) — a display that keeps changing on a fast cadence (an animated spinner, a sub-second live counter) may never go quiet within your timeout. Use a fixed `wait` or `wait_for.text` targeting a specific label instead.
- **A confusing "Error starting ttyd" / "Error launching browser" almost always means a missing dependency, not a bug.** Run `skills/termvis/scripts/check-deps.sh` to check for `ttyd` and a Chrome/Chromium browser before debugging further.

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

## Installing This Skill Elsewhere

The skill package (this file's mirror at `skills/termvis/`, its `references/`, and `scripts/check-deps.sh`) is embedded in the `termvis` binary itself, so it's available regardless of how the binary was obtained (`go install`, a release tarball, a local build):

- `termvis skill install` — installs to `~/.agents/skills/termvis` (`-project` for `./.agents/skills/termvis`, `-dir <root>` for another harness's convention, e.g. `-dir ~/.claude/skills`, `-dest <path>` for a fully custom path).
- `termvis skill show` — prints the skill without installing it.
