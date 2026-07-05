package main

import (
	"context"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type CloseSessionInput struct {
	SessionID string `json:"session_id" jsonschema:"the session identifier"`
}

type CloseSessionOutput struct {
	Status string `json:"status" jsonschema:"success or error status"`
}

func CloseSession(
	ctx context.Context,
	req *mcp.CallToolRequest,
	input CloseSessionInput,
) (*mcp.CallToolResult, CloseSessionOutput, error) {
	mu.Lock()
	s, ok := sessions[input.SessionID]
	delete(sessions, input.SessionID)
	mu.Unlock()

	if !ok {
		return nil, CloseSessionOutput{Status: "error"}, fmt.Errorf("session %q not found", input.SessionID)
	}

	if err := terminateSession(s); err != nil {
		return nil, CloseSessionOutput{Status: "error"}, fmt.Errorf("failed to close termvis session: %v", err)
	}

	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf("session %q closed", input.SessionID)}},
	}, CloseSessionOutput{Status: "success"}, nil
}
