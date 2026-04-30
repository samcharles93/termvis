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

Requires `ttyd` and a Chrome/Chromium-based browser on the host.

## Execution Model

1. **Launch:** Run `termvis [flags] -- [command]` (e.g. `termvis -- htop`, or `termvis -o demo.gif -- bash` to record a shell session).
2. **REPL Loop:**
   - **Send Action:** Output a JSON line to `stdin`.
   - **Receive State:** Read a JSON response from `stdout`.
   - **Observe (optional):** If you're driving the session interactively, decode the `image` field (Base64 PNG) to inspect terminal state. If you're just generating a GIF you can ignore responses.

## Command Schema (JSON)

Every command is a single-line JSON object:

```json
{
  "action": "string",        // "type", "key", "ctrl", "enter"
  "value": "string",         // The text to type or key name (e.g., "down", "C")
  "repeat": 1,               // Optional: repeat the action N times
  "snapshot": true,          // Optional: request an image snapshot of the result
  "wait": "200ms",           // Optional: pause for UI stability before snapshot
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

## Best Practices

- **Stability:** Always include a `wait` (e.g., `"200ms"` or `"500ms"`) when requesting a snapshot after an action that causes a UI repaint.
- **Verification:** Request `snapshot: true` whenever you need to confirm the UI has transitioned to the expected state.
- **Batched Keys:** Use `"repeat": 5` to navigate menus or lists quickly instead of sending 5 separate JSON lines.
- **Frame timing:** When a snapshot is recorded to GIF, its frame duration is taken from `wait`. Use realistic values (e.g. `"300ms"`–`"800ms"`) to keep playback readable.
- **Visible typing:** For screencasts and demo GIFs, set `typing_delay` (e.g. `"60ms"`) so individual keystrokes are observable rather than appearing as a single paste. Combine with `-i 80ms` to capture continuous frames during typing.
- **Cleanup:** The tool handles PTY cleanup automatically on exit. To exit, send a `ctrl` + `c` action or exit the shell.

## Vision Analysis

When analyzing the `image` returned in the response (only relevant when an agent is interactively driving the session, not for fire-and-forget GIF recording):
1. **Decode:** The `image` is a Base64-encoded PNG.
2. **Layout:** The image is 1:1 mapped to the pixel dimensions you specified at launch.
3. **Attributes:** Look for color changes (background highlights) to identify the currently focused element or selected menu item.

## Recording & Playback

- **Record:** Add `-o session.gif` to save the run. Combine with `-i 80ms` to capture continuously while keystrokes happen, so the resulting GIF shows typing motion rather than only before/after states.
- **Play back:** `termvis -v session.gif` renders an existing GIF inline using the Kitty graphics protocol (Ctrl+C to stop). Useful for previewing a recording without leaving the terminal.

## Worked Examples

See [`references/examples.md`](references/examples.md) for canonical JSONL recipes — generating a README demo GIF, navigating a menu-driven TUI, smoke-testing a CLI, live observation, and recovering from unexpected UI states.
