# Instructions for AI Agents: using `termvis`

`termvis` is your interface for interacting with the terminal in real-time. It allows you to "see" the terminal state as an image and send keystrokes to interact with it.

## Execution Model

1. **Launch:** Run `termvis [flags] -- [command]`.
2. **REPL Loop:**
   - **Send Action:** Output a JSON line to `stdin`.
   - **Receive State:** Read a JSON response from `stdout`.
   - **Observe:** Analyze the `image` (Base64 PNG) to decide your next move.

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
- **Visible typing:** For agent-authored screencasts, set `typing_delay` (e.g. `"60ms"`) so individual keystrokes are observable rather than appearing as a single paste.
- **Cleanup:** The tool handles PTY cleanup automatically on exit. To exit, send a `ctrl` + `c` action or exit the shell.

## Vision Analysis

When analyzing the `image` returned in the response:
1. **Decode:** The `image` is a Base64-encoded PNG.
2. **Layout:** The image is 1:1 mapped to the pixel dimensions you specified at launch.
3. **Attributes:** Look for color changes (background highlights) to identify the currently focused element or selected menu item.
