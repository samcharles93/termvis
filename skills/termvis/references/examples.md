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

- Pick a `--width`/`--height` that matches your README's expected display width.
- A `typing_delay` of `40ms`–`80ms` reads as natural typing; below 30ms looks robotic.
- For long-running commands, raise `wait` so the final state lingers on screen.

## 2. Capture a single PNG for documentation

For static screenshots in docs, request one snapshot and ignore the rest. The base64 PNG comes back on stdout — pipe it through `jq` and `base64 -d` to write a file.

```bash
(
  echo '{"action": "type",  "value": "mycli status"}'
  echo '{"action": "enter", "snapshot": true, "wait": "500ms"}'
) | termvis -- bash | jq -r 'select(.image) | .image' | base64 -d > status.png
```

## 3. Smoke-test a shell command

Verify a command produces the expected output. Snapshot the final state and have the agent assert against the rendered text.

```bash
(
  echo '{"action": "type",  "value": "echo hello world"}'
  echo '{"action": "enter", "snapshot": true, "wait": "300ms"}'
) | termvis -- bash
```

The agent reads the response's `image` field, decodes it, and confirms the rendered text contains `hello world`.

## 4. Navigate a menu-driven TUI

Pattern for scripted interaction with `fzf`, `htop`, `lazygit`, installers, etc. Use `repeat` to batch arrow keys, and snapshot after each meaningful state change.

```bash
(
  echo '{"action": "type", "value": "htop"}'
  echo '{"action": "enter", "wait": "500ms", "snapshot": true}'
  echo '{"action": "key", "value": "down", "repeat": 5, "wait": "200ms", "snapshot": true}'
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
