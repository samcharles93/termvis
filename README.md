# termvis

`termvis` is an interactive bridge that allows LLMs to "see" and interact with a terminal window.

![termvis demo](demo.png)

`termvis` uses a headless browser-backed rendering engine, it captures snapshots of terminal applications, ensuring that complex layouts, colors, and ANSI escape codes are preserved exactly as you see them.

## Key Features

- Render CLI/TUI tools in a headless browser via `xterm.js` for visual integrity when testing and debugging.
- JSONL-based stream on `stdin`/`stdout` for real-time observation and actions.
- Live view rendered right in your terminal<br><sub>*Note: Your terminal needs Kitty protocol support, like [Ghostty](https://ghostty.org/), for this to work.*</sub>
- Record sessions to GIF (`-o session.gif`) and play them back inline (`-v session.gif`) — uses a custom NRGBA palette per recording for crisp text and faithful colours.
- Designed so that multimodal AI agents can navigate and test TUI applications (see [AGENTS.md](AGENTS.md) for agent instructions).

## Installation

```bash
go install github.com/samcharles93/termvis@latest
```

*Note: Requires `ttyd` and a Chrome/Chromium-based browser installed on the host.*

To install the bundled [agent skill](skills/termvis/) itself (so agent harnesses that read `~/.agents/skills` can discover it), run `termvis skill install` — the skill is embedded in the binary, so this works no matter how you got `termvis`. Use `-project` to install to `./.agents/skills/termvis` instead, `-dir <root>` to target a different harness's skills root (e.g. `-dir ~/.claude/skills`), or `termvis skill show` to print it without installing.

## Usage

### For Observation

Watch an agent (or a script) interact with the terminal in real-time:

```bash
./termvis --watch --interval 200ms
```

### Agentic REPL

Agents send JSON commands via `stdin` and receive state snapshots via `stdout`:

```bash
# Start termvis and send eval commands via stdin
(
  echo '{"action": "type", "value": "echo hello world", "typing_delay": "60ms"}'
  echo '{"action": "enter", "snapshot": true, "wait": "400ms"}'
  echo '{"action": "ctrl", "value": "c"}'
) | ./termvis -i 80ms -o test.gif bash
```

**Input:**

```json
{"action": "type", "value": "ls -la", "snapshot": true}
{"action": "enter", "snapshot": true, "wait": "200ms"}
```

**Output:**

```json
{"status": "success", "image": "iVBORw0KGgoAAAANSUhEUgAA..."}
```

### Recording & Playback

Record any session to a GIF with `-o`, optionally with continuous capture via `-i` so the actual typing motion is preserved instead of just before/after states:

```bash
# Record at ~12fps while typing at 60ms per keystroke
./termvis -i 80ms -o session.gif bash < script.jsonl
```

Play a recorded GIF inline using the Kitty graphics protocol (Ctrl+C to exit):

```bash
./termvis -v session.gif
```

## CLI Flags

| Flag | Default | Description |
| :--- | :--- | :--- |
| `--width` | `1200` | width in pixels |
| `--height` | `600` | height in pixels |
| `--font-size` | `16` | Font size in pixels |
| `--font-family` | `JetBrains Mono` | Monospace font family |
| `--watch`, `-w` | `false` | Render snapshots in the terminal |
| `--interval`, `-i` | `0` | snapshot interval (e.g., `500ms`) |
| `--output`, `-o` | *(unset)* | Save the session to a GIF file |
| `--view`, `-v` | *(unset)* | Play a GIF file inline (skips PTY launch) |
| `--type-delay` | `0` | Default delay between keystrokes for `type` actions (e.g., `40ms`) |

## How it Works

1. The PTY engine spawns the target application inside a `ttyd` instance.
2. `ttyd` streams the terminal to an embedded `xterm.js` instance within a headless browser.
3. `go-rod` takes screenshots of the browser viewport.
4. If `--watch` is enabled, snapshots are streamed to your terminal using native Kitty APC sequences.
5. If `--output` is set, frames are quantised against an incrementally-built NRGBA palette (shared across all frames) and encoded with the stdlib `image/gif` package.
6. `--view` decodes a GIF, composites each frame onto a persistent canvas (so partial-frame GIFs from other tools render correctly), and renders via the same Kitty protocol path as `--watch`.

## MCP Server

`termvis mcp` runs termvis as an MCP server exposing `open_session`, `send_action`, and `close_session` tools, so any MCP-compatible agent can drive it without shelling out to the JSONL protocol directly. It's a single binary — like `gopls` — with no separate install step.

```bash
termvis mcp                    # stdio, for MCP clients that spawn the process directly
termvis mcp -http :8080        # Streamable HTTP transport (SSE-based), for running as a standalone service
```

**Security:** `open_session` runs arbitrary shell commands, and `-http` has no built-in authentication. Only expose it behind your own auth (reverse proxy, mTLS) or inside a network-isolated sandbox — treat it as unauthenticated remote code execution otherwise.

## Agent Skill

A skill is bundled at [`skills/termvis/`](skills/termvis/) for installation into skills-compatible agents. The skill mirrors [`AGENTS.md`](AGENTS.md) — that file remains the source of truth.
