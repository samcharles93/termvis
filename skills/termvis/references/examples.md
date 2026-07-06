# termvis Examples

Canonical JSONL recipes for common tasks. Each block is a complete script you can pipe into `termvis`.

## 1. Generate a demo GIF for a README

The most common ask: "record a GIF of my CLI doing the thing, suitable for embedding in `README.md`." Use `-o` to write the file, `-i` for continuous capture (so typing is visible mid-stroke, not just before/after), and `typing_delay` for natural-looking keystrokes. Frame durations on snapshots come from each step's `wait`.

```bash
(
  echo '{"action": "type",  "value": "mycli --help",          "typing_delay": "60ms"}'
  echo '{"action": "enter", "snapshot": true,                 "wait": "800ms"}'
  echo '{"action": "type",  "value": "mycli build ./project", "typing_delay": "60ms"}'
  echo '{"action": "enter", "snapshot": true,                 "wait": "1500ms"}'
) | termvis -i 80ms -o demo.gif -- bash
```

Preview the result inline before committing it:

```bash
termvis -v demo.gif
```

Tips for a polished GIF:

- Pick a `--width`/`--height` that matches your README's expected display width. If you're instead matching a specific TUI layout (a target cols x rows), use `--cols`/`--rows` — they size the terminal in character cells directly instead of making you compute pixels.
- A `typing_delay` of `40ms` to `80ms` reads as natural typing. Below 30ms looks robotic.
- For long-running commands, raise `wait` so the final state lingers on screen.

## 2. Capture a single PNG for documentation

For static screenshots in docs, use `save` to write the PNG straight to disk. No base64 decoding round-trip needed.

```bash
(
  echo '{"action": "type",  "value": "mycli status"}'
  echo '{"action": "enter", "save": "status.png", "wait": "500ms"}'
) | termvis -- bash
```

Piping into another tool instead? Request `"snapshot": true` and decode the response's base64 `image` field: `jq -r 'select(.image) | .image' | base64 -d`.

## 3. Smoke-test a shell command

Verify a command produces the expected output. For pure text-content assertions, request `text` instead of `snapshot`. It's an exact dump of the terminal's buffer via xterm.js, not OCR, so it's cheaper and more reliable than reading it off a screenshot.

```bash
(
  echo '{"action": "type",  "value": "echo hello world"}'
  echo '{"action": "enter", "text": true, "wait_for": {"stable": true}}'
) | termvis -- bash
```

The agent reads the response's `text` field and confirms it contains `hello world`, no vision call required. Reach for `snapshot`/`image` instead when the assertion is about color, layout, or cursor position rather than text content.

## 4. Navigate a menu-driven TUI

Pattern for scripted interaction with `fzf`, `htop`, `lazygit`, installers, etc. Use `repeat` to batch arrow keys, and snapshot after each meaningful state change. Prefer `wait_for` over a guessed `wait` when the redraw time is unpredictable, since it varies with terminal size, host load, and list length:

```bash
(
  echo '{"action": "type", "value": "htop"}'
  echo '{"action": "enter", "wait_for": {"stable": true}, "snapshot": true}'
  echo '{"action": "key", "value": "down", "repeat": 5, "wait_for": {"stable": true}, "snapshot": true}'
  echo '{"action": "key", "value": "f10"}'
) | termvis -- bash
```

## 5. Continuous observation (no recording)

Stream snapshots to your terminal in real time without producing a file. Useful when an agent (or you) is interacting with a long-running TUI and you want to watch over its shoulder.

```bash
termvis -w -i 200ms -- htop
```

## 6. Recover from an unexpected state

If a snapshot reveals the UI is not where you expected (e.g. a modal opened), send `escape` and re-snapshot before continuing the script:

```json
{"action": "key", "value": "escape", "wait": "200ms", "snapshot": true}
```

Always re-verify state with a fresh snapshot before issuing further input.

## 7. Wait for a condition instead of guessing a fixed delay

`wait` is a blind sleep. Too short and you snapshot mid-repaint; too long and every script runs slower than it needs to. `wait_for` polls the terminal's text buffer instead. `stable` waits until the buffer stops changing, meaning the render has settled, and `text` waits until a specific substring appears, like a prompt or a completion message. Both take an optional `timeout` (default `2s`). If it elapses, the response comes back with `"timed_out": true` instead of an error, and the agent decides whether to proceed, snapshot for a look, or wait again.

```bash
(
  echo '{"action": "type",  "value": "npm install"}'
  echo '{"action": "enter", "wait_for": {"text": "added", "timeout": "30s"}, "text": true, "snapshot": true}'
) | termvis -- bash
```

If the install hangs, you still get a response at the 30s mark with `timed_out: true` plus whatever `text`/`image` was captured. That's enough to see what's on screen instead of failing blind.

## 8. Drive termvis via MCP tool calls

When termvis is registered as an MCP server (see the main skill doc's "MCP Server" section), the same protocol is exposed as tools instead of JSONL. `send_action`'s `action` argument is the exact JSON schema described above, so everything from `wait_for` to `save` works identically. The calls below are illustrative (`tool_name({args})`), not literal wire format:

```
open_session({"session_id": "demo", "command": "bash"})

send_action({
  "session_id": "demo",
  "action": {"action": "type", "value": "echo hello world"}
})

send_action({
  "session_id": "demo",
  "action": {"action": "enter", "wait_for": {"stable": true}, "text": true}
})
# -> text content: "...$ echo hello world\nhello world\n$ "

close_session({"session_id": "demo"})
```

If you're resuming after a context gap and aren't sure whether a session from earlier is still open, check before opening a new one:

```
list_sessions({})
# -> "demo: bash"  (or "no open sessions")
```

Always `close_session` when done. An abandoned session leaves its `ttyd`/browser process running until the whole MCP server exits.
