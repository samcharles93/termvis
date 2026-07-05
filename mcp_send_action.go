package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type SendActionInput struct {
	SessionID string `json:"session_id" jsonschema:"the session identifier"`
	Action    Step   `json:"action" jsonschema:"the action to send to termvis"`
}

type SendActionOutput struct {
	Status string `json:"status" jsonschema:"success or error status"`
}

func SendAction(
	ctx context.Context,
	req *mcp.CallToolRequest,
	input SendActionInput,
) (*mcp.CallToolResult, SendActionOutput, error) {
	mu.Lock()
	s, ok := sessions[input.SessionID]
	mu.Unlock()

	if !ok {
		return nil, SendActionOutput{Status: "error"}, fmt.Errorf("session %q not found", input.SessionID)
	}

	// Serialize the write+decode round trip per session: two concurrent
	// SendAction calls against the same session would otherwise interleave
	// writes on the shared stdin pipe or steal each other's response line.
	s.ioMu.Lock()
	defer s.ioMu.Unlock()

	select {
	case <-s.done:
		removeSession(input.SessionID, s)
		return nil, SendActionOutput{Status: "error"}, fmt.Errorf("session %q has exited: %v", input.SessionID, s.exitErr)
	default:
	}

	data, err := json.Marshal(input.Action)
	if err != nil {
		return nil, SendActionOutput{Status: "error"}, fmt.Errorf("failed to marshal action: %v", err)
	}

	if _, err := fmt.Fprintf(s.Stdin, "%s\n", data); err != nil {
		removeSession(input.SessionID, s)
		return nil, SendActionOutput{Status: "error"}, fmt.Errorf("failed to write to termvis (session may have exited): %v", err)
	}

	var resp Response
	if err := s.Decoder.Decode(&resp); err != nil {
		removeSession(input.SessionID, s)
		return nil, SendActionOutput{Status: "error"}, fmt.Errorf("failed to read response from termvis (session may have exited): %v", err)
	}

	if resp.Status == "error" {
		return nil, SendActionOutput{Status: "error"}, fmt.Errorf("termvis error: %s", resp.Error)
	}

	var content []mcp.Content

	if resp.Image != "" {
		imgData, err := base64.StdEncoding.DecodeString(resp.Image)
		if err != nil {
			return nil, SendActionOutput{Status: "error"}, fmt.Errorf("failed to decode image: %v", err)
		}
		content = append(content, &mcp.ImageContent{
			Data:     imgData,
			MIMEType: "image/png",
		})
	}

	if resp.Text != "" {
		content = append(content, &mcp.TextContent{Text: resp.Text})
	}

	if resp.SavedTo != "" {
		content = append(content, &mcp.TextContent{Text: fmt.Sprintf("snapshot saved to %s", resp.SavedTo)})
	}

	if resp.TimedOut {
		content = append(content, &mcp.TextContent{Text: "wait_for timed out before the condition was met"})
	}

	if len(content) == 0 {
		content = []mcp.Content{&mcp.TextContent{Text: "action executed successfully"}}
	}

	return &mcp.CallToolResult{Content: content}, SendActionOutput{Status: "success"}, nil
}
