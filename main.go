package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"flag"
	"fmt"
	"image"
	"image/color/palette"
	"image/draw"
	"image/gif"
	"image/png"
	"io"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/input"
	"github.com/go-rod/rod/lib/launcher"
	"github.com/go-rod/rod/lib/proto"
)

type Step struct {
	Action   string `json:"action"`
	Value    string `json:"value,omitempty"`
	Repeat   int    `json:"repeat,omitempty"`
	Snapshot bool   `json:"snapshot,omitempty"`
	Wait     string `json:"wait,omitempty"`
}

type Response struct {
	Status string `json:"status"`
	Image  string `json:"image,omitempty"`
	Error  string `json:"error,omitempty"`
}

func randomPort() int {
	addr, _ := net.Listen("tcp", ":0")
	_ = addr.Close()
	return addr.Addr().(*net.TCPAddr).Port
}

func main() {
	fs := flag.NewFlagSet("termvis", flag.ExitOnError)
	width := fs.Int("width", 1200, "terminal width")
	height := fs.Int("height", 600, "terminal height")
	fontSize := fs.Int("font-size", 16, "font size")
	fontFamily := fs.String("font-family", "JetBrains Mono", "font family")
	watch := fs.Bool("watch", false, "render snapshots natively in terminal via Kitty protocol")
	fs.BoolVar(watch, "w", false, "alias for -watch")
	interval := fs.Duration("interval", 0, "automatic snapshot interval (e.g. 500ms)")
	fs.DurationVar(interval, "i", 0, "alias for -interval")

	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, "termvis - Interactive Multimodal TUI Testing Utility\n\n")
		fmt.Fprintf(os.Stderr, "Usage:\n  termvis [flags] [--] [command]\n\n")
		fmt.Fprintf(os.Stderr, "Flags:\n")
		fs.PrintDefaults()
		fmt.Fprintf(os.Stderr, "\nExample:\n  termvis -w -i 200ms htop\n")
	}

	fs.Parse(os.Args[1:])

	cmdArgs := fs.Args()
	shellCmd := strings.Join(cmdArgs, " ")
	if shellCmd == "" {
		shellCmd = os.Getenv("SHELL")
		if shellCmd == "" {
			shellCmd = "bash"
		}
	}

	// Signal handling for clean exit
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sigChan
		if *watch {
			cleanupPreview()
		}
		os.Exit(0)
	}()

	if *watch {
		defer cleanupPreview()
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
	defer tty.Process.Kill()

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
	page.MustSetViewport(*width, *height, 1.0, false)

	// Wait for xterm.js to be ready and apply sharp settings
	page.MustWait(`() => window.term != undefined`)
	page.MustEval(fmt.Sprintf(`() => {
		term.options.fontSize = %d
		term.options.fontFamily = '%s'
		term.options.cursorBlink = false
		term.fit()
	}`, *fontSize, *fontFamily))

	decoder := json.NewDecoder(os.Stdin)
	encoder := json.NewEncoder(os.Stdout)

	// Background watch loop
	if *interval > 0 {
		go func() {
			ticker := time.NewTicker(*interval)
			for range ticker.C {
				buf, err := captureRawSnapshot(page)
				if err == nil && *watch {
					previewSnapshot(buf)
				}
			}
		}()
	}

	for {
		var step Step
		if err := decoder.Decode(&step); err != nil {
			if err == io.EOF {
				break
			}
			encoder.Encode(Response{Status: "error", Error: fmt.Sprintf("invalid input: %v", err)})
			continue
		}

		if err := executeAction(page, step); err != nil {
			encoder.Encode(Response{Status: "error", Error: err.Error()})
			continue
		}

		if step.Wait != "" {
			d, err := time.ParseDuration(step.Wait)
			if err == nil {
				time.Sleep(d)
			}
		}

		resp := Response{Status: "success"}
		if step.Snapshot {
			buf, err := captureRawSnapshot(page)
			if err != nil {
				encoder.Encode(Response{Status: "error", Error: fmt.Sprintf("snapshot failed: %v", err)})
				continue
			}
			resp.Image = base64.StdEncoding.EncodeToString(buf)
			if *watch {
				previewSnapshot(buf)
			}
		}
		encoder.Encode(resp)
	}
}

func captureRawSnapshot(page *rod.Page) ([]byte, error) {
	el := page.MustElement(".xterm-screen")
	return el.Screenshot(proto.PageCaptureScreenshotFormatPng, 100)
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

func executeAction(page *rod.Page, step Step) error {
	repeat := step.Repeat
	if repeat <= 0 {
		repeat = 1
	}

	switch strings.ToLower(step.Action) {
	case "type":
		for i := 0; i < repeat; i++ {
			for _, r := range step.Value {
				if k, ok := keymap[r]; ok {
					page.Keyboard.MustType(k)
				} else {
					// Fallback to text input for unknown characters
					page.MustElement("textarea").MustInput(string(r))
				}
			}
		}
	case "key":
		if k, ok := specialKeyMap[strings.ToLower(step.Value)]; ok {
			for i := 0; i < repeat; i++ {
				page.Keyboard.MustType(k)
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
			}
		} else {
			return fmt.Errorf("unknown key for ctrl: %s", step.Value)
		}
	case "enter":
		for i := 0; i < repeat; i++ {
			page.Keyboard.MustType(input.Enter)
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
