package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
	"syscall"
	"time"
)

// Session represents a running termvis process.
type Session struct {
	Cmd     *exec.Cmd
	Stdin   io.WriteCloser
	Decoder *json.Decoder

	// ioMu serializes the write+decode round trip so concurrent SendAction
	// calls against the same session can't interleave writes or steal each
	// other's response line off the shared pipe.
	ioMu sync.Mutex

	// done is closed once the termvis process has exited; exitErr holds the
	// result of Cmd.Wait and is set before done is closed.
	done    chan struct{}
	exitErr error
}

var (
	sessions = make(map[string]*Session)
	mu       sync.Mutex
)

// removeSession drops a session from the registry if it's still the current
// entry for that ID — a concurrent OpenSession may already have replaced it.
func removeSession(id string, s *Session) {
	mu.Lock()
	if sessions[id] == s {
		delete(sessions, id)
	}
	mu.Unlock()
}

// resolveTermvisPath finds the termvis binary to launch: an explicit
// TERMVIS_PATH override takes precedence, falling back to a PATH lookup.
func resolveTermvisPath() (string, error) {
	if p := os.Getenv("TERMVIS_PATH"); p != "" {
		return p, nil
	}
	p, err := exec.LookPath("termvis")
	if err != nil {
		return "", fmt.Errorf(`termvis binary not found: set TERMVIS_PATH or put "termvis" on PATH`)
	}
	return p, nil
}

// terminateSession asks termvis to exit gracefully (SIGTERM) so its own
// cleanup path can kill ttyd and close the browser, falling back to SIGKILL
// only if it doesn't exit in time. Killing it outright would bypass that
// cleanup and leak the ttyd/chrome child processes.
func terminateSession(s *Session) error {
	s.Stdin.Close()

	select {
	case <-s.done:
		return nil
	default:
	}

	if err := s.Cmd.Process.Signal(syscall.SIGTERM); err != nil && !errors.Is(err, os.ErrProcessDone) {
		return err
	}

	select {
	case <-s.done:
		return nil
	case <-time.After(5 * time.Second):
		if err := s.Cmd.Process.Kill(); err != nil && !errors.Is(err, os.ErrProcessDone) {
			return err
		}
		<-s.done
		return nil
	}
}

// closeAllSessions terminates every open session. Used on server shutdown so
// restarting or killing termvis-mcp doesn't orphan ttyd/chrome processes.
func closeAllSessions() {
	mu.Lock()
	all := make([]*Session, 0, len(sessions))
	for id, s := range sessions {
		all = append(all, s)
		delete(sessions, id)
	}
	mu.Unlock()

	var wg sync.WaitGroup
	for _, s := range all {
		wg.Add(1)
		go func(s *Session) {
			defer wg.Done()
			terminateSession(s)
		}(s)
	}
	wg.Wait()
}

// Step matches the Step struct in termvis.
type Step struct {
	Action      string   `json:"action"`
	Value       string   `json:"value,omitempty"`
	Repeat      int      `json:"repeat,omitempty"`
	Snapshot    bool     `json:"snapshot,omitempty"`
	Save        string   `json:"save,omitempty"`
	Text        bool     `json:"text,omitempty"`
	Wait        string   `json:"wait,omitempty"`
	WaitFor     *WaitFor `json:"wait_for,omitempty"`
	TypingDelay string   `json:"typing_delay,omitempty"`
}

// WaitFor matches the WaitFor struct in termvis.
type WaitFor struct {
	Text    string `json:"text,omitempty"`
	Stable  bool   `json:"stable,omitempty"`
	Timeout string `json:"timeout,omitempty"`
}

// TermvisResponse matches the Response struct in termvis.
type TermvisResponse struct {
	Status   string `json:"status"`
	Image    string `json:"image,omitempty"`
	Text     string `json:"text,omitempty"`
	SavedTo  string `json:"saved_to,omitempty"`
	TimedOut bool   `json:"timed_out,omitempty"`
	Error    string `json:"error,omitempty"`
}
