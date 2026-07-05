package main

import (
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"sync"
	"syscall"
	"time"
)

// Session represents a running termvis worker process — the same binary,
// re-invoked via self-exec in its normal (non-mcp) mode.
type Session struct {
	Cmd     *exec.Cmd
	Stdin   io.WriteCloser
	Decoder *json.Decoder

	// Command is the shell command this session was opened with, kept only
	// for list_sessions to report back to a caller that's lost track of it.
	Command string

	// ioMu serializes the write+decode round trip so concurrent SendAction
	// calls against the same session can't interleave writes or steal each
	// other's response line off the shared pipe.
	ioMu sync.Mutex

	// done is closed once the worker process has exited; exitErr holds the
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

// terminateSession asks the worker to exit gracefully (SIGTERM) so its own
// cleanup path can kill ttyd and close the browser, falling back to SIGKILL
// only if it doesn't exit in time. Killing it outright would bypass that
// cleanup and leak the ttyd/chrome child processes.
func terminateSession(s *Session) error {
	_ = s.Stdin.Close()

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
// restarting or killing `termvis mcp` doesn't orphan ttyd/chrome processes.
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
			_ = terminateSession(s)
		}(s)
	}
	wg.Wait()
}
