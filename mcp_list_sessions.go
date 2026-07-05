package main

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type ListSessionsInput struct{}

type SessionInfo struct {
	SessionID string `json:"session_id"`
	Command   string `json:"command"`
}

type ListSessionsOutput struct {
	Sessions []SessionInfo `json:"sessions"`
}

// ListSessions reports currently open sessions, so a caller that's lost
// track of a session_id (context compaction, crashed mid-task) can recover
// it or close it instead of leaking the worker process. Opportunistically
// prunes sessions whose worker has already exited.
func ListSessions(
	ctx context.Context,
	req *mcp.CallToolRequest,
	input ListSessionsInput,
) (*mcp.CallToolResult, ListSessionsOutput, error) {
	mu.Lock()
	infos := make([]SessionInfo, 0, len(sessions))
	for id, s := range sessions {
		select {
		case <-s.done:
			delete(sessions, id)
			continue
		default:
		}
		infos = append(infos, SessionInfo{SessionID: id, Command: s.Command})
	}
	mu.Unlock()

	sort.Slice(infos, func(i, j int) bool { return infos[i].SessionID < infos[j].SessionID })

	text := "no open sessions"
	if len(infos) > 0 {
		lines := make([]string, len(infos))
		for i, info := range infos {
			lines[i] = fmt.Sprintf("%s: %s", info.SessionID, info.Command)
		}
		text = strings.Join(lines, "\n")
	}

	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: text}},
	}, ListSessionsOutput{Sessions: infos}, nil
}
