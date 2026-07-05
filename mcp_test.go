package main

import (
	"context"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestMCPToolRegistration(t *testing.T) {
	ctx := context.Background()

	server := mcp.NewServer(&mcp.Implementation{
		Name:    "termvis",
		Version: "v1.0.0",
	}, nil)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "open_session",
		Description: "Start a new termvis session",
	}, OpenSession)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "send_action",
		Description: "Send an action to a termvis session",
	}, SendAction)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "close_session",
		Description: "Close a termvis session",
	}, CloseSession)

	serverTransport, clientTransport := mcp.NewInMemoryTransports()

	serverSession, err := server.Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatalf("connect server: %v", err)
	}
	defer func() { _ = serverSession.Close() }()

	client := mcp.NewClient(&mcp.Implementation{
		Name:    "test-client",
		Version: "v1.0.0",
	}, nil)
	clientSession, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatalf("connect client: %v", err)
	}
	defer func() { _ = clientSession.Close() }()

	res, err := clientSession.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("list tools: %v", err)
	}

	expected := map[string]bool{
		"open_session":  true,
		"send_action":   true,
		"close_session": true,
	}

	for _, tool := range res.Tools {
		delete(expected, tool.Name)
	}

	if len(expected) > 0 {
		t.Errorf("missing tools: %v", expected)
	}
}
