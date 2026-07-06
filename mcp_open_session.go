package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type OpenSessionInput struct {
	SessionID string   `json:"session_id" jsonschema:"unique identifier for the session"`
	Command   string   `json:"command" jsonschema:"terminal command to run (e.g. bash)"`
	Args      []string `json:"args,omitempty" jsonschema:"additional arguments for termvis (e.g. --watch, --cols 110 --rows 35 to size the terminal in character cells; --width/--height are browser viewport pixels, not terminal cells)"`
}

type OpenSessionOutput struct {
	Status string `json:"status" jsonschema:"success or error status"`
}

func OpenSession(
	ctx context.Context,
	req *mcp.CallToolRequest,
	input OpenSessionInput,
) (*mcp.CallToolResult, OpenSessionOutput, error) {
	mu.Lock()
	if _, ok := sessions[input.SessionID]; ok {
		mu.Unlock()
		return nil, OpenSessionOutput{Status: "error"}, fmt.Errorf("session %q already exists", input.SessionID)
	}
	mu.Unlock()

	selfPath, err := os.Executable()
	if err != nil {
		return nil, OpenSessionOutput{Status: "error"}, fmt.Errorf("failed to resolve own executable path: %v", err)
	}

	args := append(append([]string{}, input.Args...), "--", input.Command)
	// exec.Command, deliberately not exec.CommandContext(ctx, ...): ctx is
	// scoped to this single tool call and is canceled the moment it returns
	// (the jsonrpc2 layer calls req.cancel() right after), which would kill
	// the worker process the instant OpenSession finished. It must outlive
	// this call — its lifetime is managed via CloseSession/closeAllSessions.
	cmd := exec.Command(selfPath, args...)

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, OpenSessionOutput{Status: "error"}, fmt.Errorf("failed to get stdin: %v", err)
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, OpenSessionOutput{Status: "error"}, fmt.Errorf("failed to get stdout: %v", err)
	}

	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		return nil, OpenSessionOutput{Status: "error"}, fmt.Errorf("failed to start termvis worker: %v", err)
	}

	sess := &Session{
		Cmd:     cmd,
		Stdin:   stdin,
		Decoder: json.NewDecoder(stdout),
		Command: input.Command,
		done:    make(chan struct{}),
	}
	go func() {
		sess.exitErr = cmd.Wait()
		close(sess.done)
	}()

	mu.Lock()
	if _, ok := sessions[input.SessionID]; ok {
		mu.Unlock()
		go func() { _ = terminateSession(sess) }()
		return nil, OpenSessionOutput{Status: "error"}, fmt.Errorf("session %q already exists", input.SessionID)
	}
	sessions[input.SessionID] = sess
	mu.Unlock()

	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf("session %q started", input.SessionID)}},
	}, OpenSessionOutput{Status: "success"}, nil
}
