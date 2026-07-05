package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// runMCPServer runs termvis as an MCP stdio server (`termvis mcp`). Each
// open_session call re-execs the current binary in its normal worker mode
// (see runTermvis) to drive one terminal session.
func runMCPServer() {
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
		Description: "Send an action (type, key, ctrl, enter) to a termvis session",
	}, SendAction)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "close_session",
		Description: "Close an active termvis session",
	}, CloseSession)

	// Ensure open termvis sessions (and their ttyd/chrome children) don't get
	// orphaned if the MCP host kills or restarts this server.
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sigChan
		closeAllSessions()
		os.Exit(0)
	}()

	err := server.Run(context.Background(), &mcp.StdioTransport{})
	closeAllSessions()
	if err != nil {
		log.Fatal(err)
	}
}
