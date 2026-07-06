package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"flag"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/gif"
	"image/png"
	"io"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"runtime/debug"
	"strings"
	"syscall"
	"time"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/input"
	"github.com/go-rod/rod/lib/launcher"
	"github.com/go-rod/rod/lib/proto"
)

// version, commit, date, and builtBy are populated at build time via
// GoReleaser's default ldflags (-X main.version=... etc.); a plain `go
// build` leaves them at these defaults.
var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
	builtBy = "source"
)

// versionInfo resolves the values to print for --version. When the ldflags
// above were never set (e.g. `go install pkg@latest`, which doesn't run
// GoReleaser), it falls back to the module/VCS info Go embeds automatically
// via runtime/debug.
func versionInfo() (v, c, d, b string) {
	v, c, d, b = version, commit, date, builtBy
	if v != "dev" {
		return
	}
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return
	}
	if info.Main.Version != "" && info.Main.Version != "(devel)" {
		v = info.Main.Version
	}
	for _, s := range info.Settings {
		switch s.Key {
		case "vcs.revision":
			if len(s.Value) >= 12 {
				c = s.Value[:12]
			} else {
				c = s.Value
			}
		case "vcs.time":
			d = s.Value
		}
	}
	b = "go install"
	return
}

type Step struct {
	Action   string `json:"action"`
	Value    string `json:"value,omitempty"`
	Repeat   int    `json:"repeat,omitempty"`
	Snapshot bool   `json:"snapshot,omitempty"`
	// Save writes the PNG snapshot directly to this file path instead of
	// (or alongside) base64-encoding it into the response's Image field.
	Save string `json:"save,omitempty"`
	// Text requests a plain-text dump of the terminal's visible buffer.
	Text bool   `json:"text,omitempty"`
	Wait string `json:"wait,omitempty"`
	// WaitFor polls the terminal's text buffer instead of sleeping a fixed
	// duration. When set, it takes precedence over Wait for this step.
	WaitFor *WaitFor `json:"wait_for,omitempty"`
	// TypingDelay overrides the global -type-delay for this step (e.g. "40ms").
	TypingDelay string `json:"typing_delay,omitempty"`
}

// WaitFor describes a condition to poll for after an action, instead of a
// fixed sleep. At least one of Text or Stable must be set.
type WaitFor struct {
	// Text waits until the visible buffer contains this substring.
	Text string `json:"text,omitempty"`
	// Stable waits until the visible buffer stops changing across several
	// consecutive polls (i.e. the render has settled).
	Stable bool `json:"stable,omitempty"`
	// Timeout overrides the default wait_for timeout (e.g. "3s").
	Timeout string `json:"timeout,omitempty"`
}

type Response struct {
	Status string `json:"status"`
	Image  string `json:"image,omitempty"`
	Text   string `json:"text,omitempty"`
	// SavedTo echoes the path an image snapshot was written to, when Save was set.
	SavedTo string `json:"saved_to,omitempty"`
	// TimedOut reports that a WaitFor condition never became true before its
	// timeout elapsed. This is not treated as an error: the caller decides
	// whether a slow render is acceptable.
	TimedOut bool   `json:"timed_out,omitempty"`
	Error    string `json:"error,omitempty"`
}

func randomPort() int {
	addr, _ := net.Listen("tcp", ":0")
	_ = addr.Close()
	return addr.Addr().(*net.TCPAddr).Port
}

func main() {
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "mcp":
			runMCPServer(os.Args[2:])
			return
		case "skill":
			runSkillCommand(os.Args[2:])
			return
		}
	}
	runTermvis()
}

// runTermvis drives a single terminal session over JSONL on stdin/stdout.
// The MCP server (see mcp.go) re-execs the current binary in this same mode
// to spawn each session's worker process.
func runTermvis() {
	flags := flag.NewFlagSet("termvis", flag.ContinueOnError)

	width := flags.Int("width", 1200, "browser viewport width in CSS pixels (not terminal columns — see -cols)")
	height := flags.Int("height", 600, "browser viewport height in CSS pixels (not terminal rows — see -rows)")

	cols := flags.Int("cols", 0, "terminal width in character cells (overrides -width when set)")
	rows := flags.Int("rows", 0, "terminal height in character cells (overrides -height when set)")

	fontSize := flags.Int("font-size", 16, "font size in pixels")
	fontFamily := flags.String("font-family", "JetBrains Mono", "font family (must be monospace for correct cell sizing)")

	watch := flags.Bool("watch", false, "render snapshots in the terminal")
	flags.BoolVar(watch, "w", false, "alias for -watch")

	interval := flags.Duration("interval", 0, "automatic snapshot interval (e.g. 500ms)")
	flags.DurationVar(interval, "i", 0, "alias for -interval")

	output := flags.String("output", "", "save recording to GIF file")
	flags.StringVar(output, "o", "", "alias for -output")

	view := flags.String("view", "", "view a GIF file in the terminal (Kitty graphics protocol)")
	flags.StringVar(view, "v", "", "alias for -view")

	typeDelay := flags.Duration("type-delay", 0, "delay between keystrokes for type actions (e.g. 40ms)")

	showVersion := flags.Bool("version", false, "print version information and exit")

	flags.Usage = func() {
		fmt.Fprint(os.Stderr, `termvis - drive a real terminal from a script or an agent

termvis runs [command] inside a real PTY rendered by xterm.js in a headless
browser, and speaks newline-delimited JSON over stdin/stdout: send
keystrokes in, get back screenshots, plain text, or a GIF recording.

Usage:
  termvis [flags] [--] [command]   Run [command] (default: $SHELL); speak JSONL on stdin/stdout
  termvis mcp [-http addr]         Run as an MCP server (stdio by default, or HTTP/SSE with -http)
  termvis skill install|show       Install or print the bundled agent skill

Protocol — one JSON object per line on stdin, one back on stdout:

  in:  {"action": "type", "value": "echo hi"}
  in:  {"action": "enter", "snapshot": true, "wait_for": {"stable": true}}
  out: {"status": "success", "image": "<base64 png>"}

  action: "type" | "key" | "ctrl" | "enter"
  value:  text to type, a key name (up/down/left/right/enter/backspace/tab/escape/space), or a ctrl letter
  optional in:  repeat, snapshot, save ("path.png"), text, wait ("200ms"), wait_for ({text|stable, timeout}), typing_delay
  optional out: image, text, saved_to, timed_out, error — each present only when relevant, never empty

Sizing:
  -width/-height  browser viewport in pixels (default 1200x600) — a rough pixel budget, not a cell count
  -cols/-rows     terminal size in character cells (recommended for TUI apps)
  If -cols or -rows is set, character-cell sizing wins for both dimensions over -width/-height.

`)
		fmt.Fprint(os.Stderr, "Flags:\n")
		flags.PrintDefaults()
		fmt.Fprint(os.Stderr, `
Examples:
  termvis -w -i 200ms -o session.gif -- htop     Record a live htop session to a GIF
  (echo '{"action":"type","value":"ls"}'; echo '{"action":"enter","snapshot":true,"wait":"300ms"}') | termvis -- bash

Full protocol reference, worked examples, and MCP tools:
  https://github.com/samcharles93/termvis
`)
	}

	if err := flags.Parse(os.Args[1:]); err != nil {
		if err != flag.ErrHelp {
			fmt.Fprintf(os.Stderr, "Error parsing flags: %v\n", err)
		}
		os.Exit(1)
	}

	if *showVersion {
		v, c, d, b := versionInfo()
		fmt.Printf("termvis %s (commit %s, built %s by %s)\n", v, c, d, b)
		return
	}

	if *width <= 0 || *height <= 0 {
		fmt.Fprintf(os.Stderr, "Error: -width and -height must be positive\n")
		os.Exit(1)
	}
	if *cols < 0 || *rows < 0 {
		fmt.Fprintf(os.Stderr, "Error: -cols and -rows must be positive\n")
		os.Exit(1)
	}

	if *view != "" {
		if err := playGIF(*view); err != nil {
			fmt.Fprintf(os.Stderr, "Error viewing GIF: %v\n", err)
			os.Exit(1)
		}
		return
	}

	cmdArgs := flags.Args()
	shellCmd := strings.Join(cmdArgs, " ")
	if shellCmd == "" {
		shellCmd = os.Getenv("SHELL")
		if shellCmd == "" {
			shellCmd = "bash"
		}
	}

	// Recorder state
	var recording *gifRecorder
	if *output != "" {
		recording = newGIFRecorder()
	}

	// Signal handling for clean exit
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sigChan
		if *watch {
			cleanupPreview()
		}
		if recording != nil && len(recording.g.Image) > 0 {
			recording.save(*output)
		}
		os.Exit(0)
	}()

	if *watch {
		defer cleanupPreview()
	}

	if recording != nil {
		defer func() {
			if len(recording.g.Image) > 0 {
				recording.save(*output)
			}
		}()
	}

	port := randomPort()
	ttyArgs := []string{
		fmt.Sprintf("--port=%d", port),
		"--interface", "127.0.0.1",
		"-t", "rendererType=canvas",
		"-t", "disableResizeOverlay=true",
		"--once",
		"--writable",
	}
	ttyArgs = append(ttyArgs, "bash", "-c", shellCmd)

	tty := exec.Command("ttyd", ttyArgs...)
	if err := tty.Start(); err != nil {
		fmt.Fprintf(os.Stderr, "Error starting ttyd: %v\n", err)
		os.Exit(1)
	}
	defer func() {
		if err := tty.Process.Kill(); err != nil && err != os.ErrProcessDone {
			fmt.Fprintf(os.Stderr, "Error killing ttyd: %v\n", err)
		}
	}()

	// Give ttyd a moment to start
	time.Sleep(500 * time.Millisecond)

	path, _ := launcher.LookPath()
	u, err := launcher.New().Leakless(false).Bin(path).NoSandbox(true).Launch()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error launching browser: %v\n", err)
		os.Exit(1)
	}
	browser := rod.New().ControlURL(u).MustConnect()
	defer browser.MustClose()

	page := browser.MustPage(fmt.Sprintf("http://localhost:%d", port))

	// Character-cell sizing (-cols/-rows) wins over pixel sizing (-width/-height)
	// for both dimensions when either is set — mixing the two coordinate
	// systems per-axis would produce surprising results.
	useCellSizing := *cols > 0 || *rows > 0
	cellCols, cellRows := *cols, *rows
	if useCellSizing {
		if cellCols == 0 {
			cellCols = 80
		}
		if cellRows == 0 {
			cellRows = 24
		}
	}

	viewW, viewH := *width, *height
	if useCellSizing {
		viewW, viewH = scratchViewport(cellCols, cellRows, *fontSize)
	}
	page.MustSetViewport(viewW, viewH, 1.0, false)

	// Wait for xterm.js to be ready and apply sharp settings
	page.MustWait(`() => window.term != undefined`)
	page.MustEval(fmt.Sprintf(`() => {
		term.options.fontSize = %d
		term.options.fontFamily = '%s'
		term.options.cursorBlink = false
	}`, *fontSize, *fontFamily))

	if useCellSizing {
		// Resize to the exact requested cell count, then measure the actual
		// rendered pixel box and snap the viewport to it — this eliminates
		// ttyd/xterm.js chrome and padding slop instead of guessing at it.
		page.MustEval(fmt.Sprintf(`() => { term.resize(%d, %d) }`, cellCols, cellRows))
		measured := page.MustEval(`() => {
			const r = document.querySelector('.xterm-screen').getBoundingClientRect()
			return {w: Math.ceil(r.width), h: Math.ceil(r.height)}
		}`)
		viewW, viewH = measured.Get("w").Int(), measured.Get("h").Int()
		page.MustSetViewport(viewW, viewH, 1.0, false)
	} else {
		page.MustEval(`() => { term.fit() }`)
	}

	termSize := page.MustEval(`() => ({cols: term.cols, rows: term.rows})`)
	finalCols, finalRows := termSize.Get("cols").Int(), termSize.Get("rows").Int()

	if finalCols < minTerminalCols || finalRows < minTerminalRows {
		fmt.Fprintf(os.Stderr, "termvis: viewport %dx%dpx is too small to fit a usable terminal at font-size %d (got %dx%d cells, need at least %dx%d) — did you mean -cols/-rows for character-cell sizing? (see --help)\n",
			viewW, viewH, *fontSize, finalCols, finalRows, minTerminalCols, minTerminalRows)
		os.Exit(1)
	}
	fmt.Fprintf(os.Stderr, "termvis: terminal ready — %dx%d cells (%dx%dpx viewport, font-size %d)\n", finalCols, finalRows, viewW, viewH, *fontSize)

	decoder := json.NewDecoder(os.Stdin)
	encoder := json.NewEncoder(os.Stdout)

	// Background watch loop
	stopInterval := make(chan struct{})
	intervalDone := make(chan struct{})
	if *interval > 0 {
		go func() {
			defer close(intervalDone)
			ticker := time.NewTicker(*interval)
			defer ticker.Stop()
			for {
				select {
				case <-stopInterval:
					return
				case <-ticker.C:
				}
				buf, err := captureRawSnapshot(page)
				if err != nil {
					continue
				}
				if *watch {
					previewSnapshot(buf)
				}
				if recording != nil {
					recording.addFrame(buf, durationToDelay(*interval))
				}
			}
		}()
		defer func() {
			close(stopInterval)
			<-intervalDone
		}()
	}

	for {
		var step Step
		if err := decoder.Decode(&step); err != nil {
			if err == io.EOF {
				break
			}
			if err := encoder.Encode(Response{Status: "error", Error: fmt.Sprintf("invalid input: %v", err)}); err != nil {
				fmt.Fprintf(os.Stderr, "Error encoding response: %v\n", err)
				continue
			}
			continue
		}

		if err := executeAction(page, step, *typeDelay); err != nil {
			if err := encoder.Encode(Response{Status: "error", Error: err.Error()}); err != nil {
				fmt.Fprintf(os.Stderr, "Error encoding response: %v\n", err)
			}
			continue
		}

		var timedOut bool
		if step.WaitFor != nil {
			if step.WaitFor.Text == "" && !step.WaitFor.Stable {
				if err := encoder.Encode(Response{Status: "error", Error: `wait_for requires "text" or "stable"`}); err != nil {
					fmt.Fprintf(os.Stderr, "Error encoding response: %v\n", err)
				}
				continue
			}
			var err error
			timedOut, err = waitForCondition(page, step.WaitFor)
			if err != nil {
				if err := encoder.Encode(Response{Status: "error", Error: err.Error()}); err != nil {
					fmt.Fprintf(os.Stderr, "Error encoding response: %v\n", err)
				}
				continue
			}
		} else if step.Wait != "" {
			d, err := time.ParseDuration(step.Wait)
			if err == nil {
				time.Sleep(d)
			}
		}

		resp := Response{Status: "success", TimedOut: timedOut}

		if step.Text {
			text, err := captureText(page)
			if err != nil {
				if err := encoder.Encode(Response{Status: "error", Error: fmt.Sprintf("text capture failed: %v", err)}); err != nil {
					fmt.Fprintf(os.Stderr, "Error encoding response: %v\n", err)
				}
				continue
			}
			resp.Text = text
		}

		if step.Snapshot || step.Save != "" {
			buf, err := captureRawSnapshot(page)
			if err != nil {
				if err := encoder.Encode(Response{Status: "error", Error: fmt.Sprintf("snapshot failed: %v", err)}); err != nil {
					fmt.Fprintf(os.Stderr, "Error encoding response: %v\n", err)
				}
				continue
			}
			if step.Snapshot {
				resp.Image = base64.StdEncoding.EncodeToString(buf)
			}
			if step.Save != "" {
				if err := os.WriteFile(step.Save, buf, 0o644); err != nil {
					if err := encoder.Encode(Response{Status: "error", Error: fmt.Sprintf("save failed: %v", err)}); err != nil {
						fmt.Fprintf(os.Stderr, "Error encoding response: %v\n", err)
					}
					continue
				}
				resp.SavedTo = step.Save
			}
			if *watch {
				previewSnapshot(buf)
			}
			if recording != nil && step.Snapshot {
				// Frame is shown for the duration that step.Wait paused before
				// capture; falls back to 500ms if Wait was unset/invalid.
				delay := 50
				if step.Wait != "" {
					if d, err := time.ParseDuration(step.Wait); err == nil {
						delay = durationToDelay(d)
					}
				}
				recording.addFrame(buf, delay)
			}
		}
		if err := encoder.Encode(resp); err != nil {
			fmt.Fprintf(os.Stderr, "Error encoding response: %v\n", err)
		}
	}
}

// gifRecorder builds a GIF using a single shared palette derived from the
// distinct NRGBA colours seen across all captured frames. Terminal output
// uses a small fixed colour set so 256 entries is almost always enough; if
// the limit is exceeded, additional colours are snapped to the nearest
// existing entry without dithering.
type gifRecorder struct {
	g       *gif.GIF
	palette color.Palette
	lut     map[color.NRGBA]uint8
}

func newGIFRecorder() *gifRecorder {
	return &gifRecorder{
		g:   &gif.GIF{},
		lut: make(map[color.NRGBA]uint8),
	}
}

func (r *gifRecorder) addFrame(data []byte, delay int) {
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		return
	}
	bounds := img.Bounds()
	paletted := image.NewPaletted(bounds, nil)

	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			c := color.NRGBAModel.Convert(img.At(x, y)).(color.NRGBA)
			idx, ok := r.lut[c]
			if !ok {
				if len(r.palette) < 256 {
					idx = uint8(len(r.palette))
					r.palette = append(r.palette, c)
				} else {
					idx = uint8(r.palette.Index(c))
				}
				r.lut[c] = idx
			}
			paletted.Pix[(y-bounds.Min.Y)*paletted.Stride+(x-bounds.Min.X)] = idx
		}
	}

	r.g.Image = append(r.g.Image, paletted)
	r.g.Delay = append(r.g.Delay, delay)
}

func (r *gifRecorder) save(path string) {
	// Point every frame at the final palette before encoding so indices
	// recorded against earlier (shorter) palette snapshots remain valid.
	for _, frame := range r.g.Image {
		frame.Palette = r.palette
	}

	f, err := os.Create(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error creating GIF: %v\n", err)
		return
	}
	defer func() {
		if cerr := f.Close(); cerr != nil {
			fmt.Fprintf(os.Stderr, "Error closing GIF: %v\n", cerr)
		}
	}()

	if err := gif.EncodeAll(f, r.g); err != nil {
		fmt.Fprintf(os.Stderr, "Error encoding GIF: %v\n", err)
	}
}

// durationToDelay converts a Go duration to GIF frame delay (1/100s units),
// clamped to a minimum of 2 because most renderers treat 0/1 as "fast as
// possible" and substitute a long default delay.
func durationToDelay(d time.Duration) int {
	delay := max(int(d/(10*time.Millisecond)), 2)
	return delay
}

func playGIF(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()

	g, err := gif.DecodeAll(f)
	if err != nil {
		return err
	}
	if len(g.Image) == 0 {
		return fmt.Errorf("GIF contains no frames")
	}

	// Composite each frame onto a persistent canvas so partial-frame GIFs
	// (common for non-termvis sources) render correctly. Our own recorder
	// emits full frames, so this is a no-op for those.
	canvas := image.NewNRGBA(image.Rect(0, 0, g.Config.Width, g.Config.Height))
	pngs := make([][]byte, len(g.Image))
	for i, frame := range g.Image {
		draw.Draw(canvas, frame.Bounds(), frame, frame.Bounds().Min, draw.Over)
		var buf bytes.Buffer
		if err := png.Encode(&buf, canvas); err != nil {
			return err
		}
		pngs[i] = buf.Bytes()
	}

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	defer cleanupPreview()

	playOnce := func() bool {
		for i, data := range pngs {
			select {
			case <-sigChan:
				return false
			default:
			}
			previewSnapshot(data)
			d := time.Duration(g.Delay[i]) * 10 * time.Millisecond
			if d <= 0 {
				d = 100 * time.Millisecond
			}
			time.Sleep(d)
		}
		return true
	}

	// LoopCount semantics per the gif package:
	//   -1: play once, no loop
	//    0: loop forever (most common, also our recorder's default)
	//   >0: play LoopCount additional times after the first
	switch {
	case g.LoopCount < 0:
		playOnce()
	case g.LoopCount == 0:
		for playOnce() {
		}
	default:
		for n := 0; n <= g.LoopCount; n++ {
			if !playOnce() {
				break
			}
		}
	}
	return nil
}

// waitForPollInterval, waitForStableRounds, and waitForDefaultTimeout tune
// the wait_for polling loop: how often the buffer is sampled, how many
// consecutive identical samples count as "stable" (render has settled), and
// how long to poll before giving up.
const (
	waitForPollInterval   = 80 * time.Millisecond
	waitForStableRounds   = 3
	waitForDefaultTimeout = 2 * time.Second
)

// minTerminalCols and minTerminalRows are the floor below which a rendered
// terminal is considered unusable rather than merely small — this is what
// catches a -width/-height pair (or -cols/-rows) too small to fit any real
// content, instead of silently returning blank "success" snapshots.
const (
	minTerminalCols = 10
	minTerminalRows = 2
)

// scratchViewportMinPx and scratchViewportMaxPx bound the scratch viewport
// computed by scratchViewport, guarding against degenerate (near-zero) or
// runaway (larger than a headless browser can reasonably handle) requests.
const (
	scratchViewportMinPx = 300
	scratchViewportMaxPx = 8000
)

// scratchViewport estimates a browser viewport, in CSS pixels, generous
// enough to render `cols` x `rows` character cells at the given font size
// without clipping or wrapping. The multipliers are deliberately generous
// overestimates of typical monospace cell metrics (real cell width usually
// runs ~0.5-0.6x font size, line height ~1.2-1.3x) — this viewport is only a
// scratch space for term.resize() to render into; the caller measures the
// actual rendered box afterward and snaps the real viewport to that.
func scratchViewport(cols, rows, fontSize int) (w, h int) {
	w = clampInt(cols*fontSize*7/10+40, scratchViewportMinPx, scratchViewportMaxPx)
	h = clampInt(rows*fontSize*14/10+40, scratchViewportMinPx, scratchViewportMaxPx)
	return w, h
}

func clampInt(v, min, max int) int {
	if v < min {
		return min
	}
	if v > max {
		return max
	}
	return v
}

// waitForCondition polls the terminal's text buffer until the requested
// condition is met or the timeout elapses. A timeout is reported via the
// returned bool, not an error: a slow render isn't necessarily a bug, and
// the caller is better placed to decide whether to proceed or retry.
func waitForCondition(page *rod.Page, wf *WaitFor) (timedOut bool, err error) {
	timeout := waitForDefaultTimeout
	if wf.Timeout != "" {
		d, perr := time.ParseDuration(wf.Timeout)
		if perr != nil {
			return false, fmt.Errorf("invalid wait_for timeout %q: %v", wf.Timeout, perr)
		}
		timeout = d
	}

	deadline := time.Now().Add(timeout)
	lastText, err := captureText(page)
	if err != nil {
		return false, err
	}
	stableCount := 1

	for {
		if wf.Text != "" && strings.Contains(lastText, wf.Text) {
			return false, nil
		}
		if wf.Stable && stableCount >= waitForStableRounds {
			return false, nil
		}
		if time.Now().After(deadline) {
			return true, nil
		}
		time.Sleep(waitForPollInterval)

		text, cerr := captureText(page)
		if cerr != nil {
			return false, cerr
		}
		if text == lastText {
			stableCount++
		} else {
			stableCount = 1
		}
		lastText = text
	}
}

// captureText reads the terminal's currently visible rows as plain text via
// xterm.js's buffer API, trimming trailing whitespace per line. This is
// exact (no OCR/vision call needed) and much cheaper than a screenshot,
// which makes it the basis for both the "text" response field and wait_for.
func captureText(page *rod.Page) (text string, err error) {
	err = rod.Try(func() {
		obj, evalErr := page.Eval(`() => {
			const buf = term.buffer.active
			const lines = []
			for (let i = 0; i < term.rows; i++) {
				const line = buf.getLine(buf.viewportY + i)
				lines.push(line ? line.translateToString(true) : '')
			}
			return lines.join('\n')
		}`)
		if evalErr != nil {
			panic(evalErr)
		}
		text = obj.Value.Str()
	})
	return
}

func captureRawSnapshot(page *rod.Page) (data []byte, err error) {
	// Wrap in rod.Try so transient page state (navigation, missing element,
	// closed connection) becomes a returned error instead of a panic that
	// would kill background goroutines and skip pending defers.
	err = rod.Try(func() {
		el := page.MustElement(".xterm-screen")
		buf, sErr := el.Screenshot(proto.PageCaptureScreenshotFormatPng, 100)
		if sErr != nil {
			panic(sErr)
		}
		data = buf
	})
	return
}

func previewSnapshot(data []byte) {
	// Native implementation of the Kitty Graphics Protocol
	// See: https://sw.kovidgoyal.net/kitty/graphics-protocol/

	// 1. Reset cursor to top-left of the current area to prevent cascading
	fmt.Fprint(os.Stderr, "\x1b[H")

	// Use i=1 to identify the image and C=1 to keep the cursor stationary
	const chunkLimit = 4096
	b64Data := base64.StdEncoding.EncodeToString(data)

	for i := 0; i < len(b64Data); i += chunkLimit {
		end := i + chunkLimit
		m := 1
		if end >= len(b64Data) {
			end = len(b64Data)
			m = 0
		}

		var control string
		if i == 0 {
			// a=T: Action Transmit and Display
			// f=100: Format PNG
			// i=1: Image ID (overwrite previous i=1)
			// C=1: Do not move cursor after drawing
			control = fmt.Sprintf("a=T,f=100,i=1,C=1,m=%d", m)
		} else {
			control = fmt.Sprintf("m=%d", m)
		}

		fmt.Fprintf(os.Stderr, "\x1b_G%s;%s\x1b\\", control, b64Data[i:end])
	}
}

func cleanupPreview() {
	// Kitty Graphics Protocol: Delete all images
	// a=d: Action Delete
	// d=A: Delete all images
	fmt.Fprint(os.Stderr, "\x1b_Ga=d,d=A\x1b\\")
	// Clear terminal and reset cursor
	fmt.Fprint(os.Stderr, "\x1b[2J\x1b[H")
}

func executeAction(page *rod.Page, step Step, defaultTypeDelay time.Duration) error {
	repeat := step.Repeat
	if repeat <= 0 {
		repeat = 1
	}

	delay := defaultTypeDelay
	if step.TypingDelay != "" {
		if d, err := time.ParseDuration(step.TypingDelay); err == nil {
			delay = d
		} else {
			return fmt.Errorf("invalid typing_delay %q: %v", step.TypingDelay, err)
		}
	}

	switch strings.ToLower(step.Action) {
	case "type":
		runes := []rune(step.Value)
		for i := 0; i < repeat; i++ {
			for j, r := range runes {
				if k, ok := keymap[r]; ok {
					page.Keyboard.MustType(k)
				} else {
					// Page.InsertText emits one CDP Input.insertText call per
					// rune, producing a single text-input event — closer to a
					// keystroke than MustInput which replaces the textarea
					// value in one event.
					page.MustInsertText(string(r))
				}
				if delay > 0 && (i != repeat-1 || j != len(runes)-1) {
					time.Sleep(delay)
				}
			}
		}
	case "key":
		if k, ok := specialKeyMap[strings.ToLower(step.Value)]; ok {
			for i := 0; i < repeat; i++ {
				page.Keyboard.MustType(k)
				if delay > 0 && i < repeat-1 {
					time.Sleep(delay)
				}
			}
		} else {
			return fmt.Errorf("unknown special key: %s", step.Value)
		}
	case "ctrl":
		if len(step.Value) != 1 {
			return fmt.Errorf("ctrl action requires exactly one character, got %q", step.Value)
		}
		val := rune(strings.ToUpper(step.Value)[0])
		if k, ok := keymap[val]; ok {
			for i := 0; i < repeat; i++ {
				page.KeyActions().Press(input.ControlLeft).Type(k).MustDo()
				if delay > 0 && i < repeat-1 {
					time.Sleep(delay)
				}
			}
		} else {
			return fmt.Errorf("unknown key for ctrl: %s", step.Value)
		}
	case "enter":
		for i := 0; i < repeat; i++ {
			page.Keyboard.MustType(input.Enter)
			if delay > 0 && i < repeat-1 {
				time.Sleep(delay)
			}
		}
	default:
		return fmt.Errorf("unsupported action: %s", step.Action)
	}
	return nil
}

// shift returns the input.Key with the shift modifier set.
func shift(k input.Key) input.Key {
	k, _ = k.Shift()
	return k
}

var specialKeyMap = map[string]input.Key{
	"enter":     input.Enter,
	"backspace": input.Backspace,
	"tab":       input.Tab,
	"escape":    input.Escape,
	"up":        input.ArrowUp,
	"down":      input.ArrowDown,
	"left":      input.ArrowLeft,
	"right":     input.ArrowRight,
	"space":     input.Space,
}

var keymap = map[rune]input.Key{
	' ':    input.Space,
	'!':    shift(input.Digit1),
	'"':    shift(input.Quote),
	'#':    shift(input.Digit3),
	'$':    shift(input.Digit4),
	'%':    shift(input.Digit5),
	'&':    shift(input.Digit7),
	'(':    shift(input.Digit9),
	')':    shift(input.Digit0),
	'*':    shift(input.Digit8),
	'+':    shift(input.Equal),
	',':    input.Comma,
	'-':    input.Minus,
	'.':    input.Period,
	'/':    input.Slash,
	'0':    input.Digit0,
	'1':    input.Digit1,
	'2':    input.Digit2,
	'3':    input.Digit3,
	'4':    input.Digit4,
	'5':    input.Digit5,
	'6':    input.Digit6,
	'7':    input.Digit7,
	'8':    input.Digit8,
	'9':    input.Digit9,
	':':    shift(input.Semicolon),
	';':    input.Semicolon,
	'<':    shift(input.Comma),
	'=':    input.Equal,
	'>':    shift(input.Period),
	'?':    shift(input.Slash),
	'@':    shift(input.Digit2),
	'A':    shift(input.KeyA),
	'B':    shift(input.KeyB),
	'C':    shift(input.KeyC),
	'D':    shift(input.KeyD),
	'E':    shift(input.KeyE),
	'F':    shift(input.KeyF),
	'G':    shift(input.KeyG),
	'H':    shift(input.KeyH),
	'I':    shift(input.KeyI),
	'J':    shift(input.KeyJ),
	'K':    shift(input.KeyK),
	'L':    shift(input.KeyL),
	'M':    shift(input.KeyM),
	'N':    shift(input.KeyN),
	'O':    shift(input.KeyO),
	'P':    shift(input.KeyP),
	'Q':    shift(input.KeyQ),
	'R':    shift(input.KeyR),
	'S':    shift(input.KeyS),
	'T':    shift(input.KeyT),
	'U':    shift(input.KeyU),
	'V':    shift(input.KeyV),
	'W':    shift(input.KeyW),
	'X':    shift(input.KeyX),
	'Y':    shift(input.KeyY),
	'Z':    shift(input.KeyZ),
	'[':    input.BracketLeft,
	'\'':   input.Quote,
	'\\':   input.Backslash,
	'\b':   input.Backspace,
	'\n':   input.Enter,
	'\r':   input.Enter,
	'\t':   input.Tab,
	'\x1b': input.Escape,
	']':    input.BracketRight,
	'^':    shift(input.Digit6),
	'_':    shift(input.Minus),
	'`':    input.Backquote,
	'a':    input.KeyA,
	'b':    input.KeyB,
	'c':    input.KeyC,
	'd':    input.KeyD,
	'e':    input.KeyE,
	'f':    input.KeyF,
	'g':    input.KeyG,
	'h':    input.KeyH,
	'i':    input.KeyI,
	'j':    input.KeyJ,
	'k':    input.KeyK,
	'l':    input.KeyL,
	'm':    input.KeyM,
	'n':    input.KeyN,
	'o':    input.KeyO,
	'p':    input.KeyP,
	'q':    input.KeyQ,
	'r':    input.KeyR,
	's':    input.KeyS,
	't':    input.KeyT,
	'u':    input.KeyU,
	'v':    input.KeyV,
	'w':    input.KeyW,
	'x':    input.KeyX,
	'y':    input.KeyY,
	'z':    input.KeyZ,
	'{':    shift(input.BracketLeft),
	'|':    shift(input.Backslash),
	'}':    shift(input.BracketRight),
	'~':    shift(input.Backquote),
}
