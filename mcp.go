package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// newMCPServer builds the MCP server and registers its tools. Called once
// for the stdio transport, and once per HTTP session for the HTTP
// transport — cheap either way, since all session state lives in the
// package-level sessions map (mcp_sessions.go), not on the *mcp.Server.
func newMCPServer() *mcp.Server {
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

	mcp.AddTool(server, &mcp.Tool{
		Name:        "list_sessions",
		Description: "List currently open termvis sessions (recover a lost session_id, or find sessions to clean up)",
	}, ListSessions)

	return server
}

// runMCPServer runs termvis as an MCP server (`termvis mcp`): over stdio by
// default, or over HTTP (Streamable HTTP transport, which streams over SSE)
// when -http is set so it can run as a standalone service on your own
// infrastructure. Each open_session call re-execs the current binary in its
// normal worker mode (see runTermvis) to drive one terminal session.
func runMCPServer(args []string) {
	flags := flag.NewFlagSet("termvis mcp", flag.ExitOnError)
	httpAddr := flags.String("http", "", "serve over HTTP (Streamable HTTP/SSE transport) on this address instead of stdio, e.g. :8080")

	flags.Usage = func() {
		fmt.Fprintf(os.Stderr, "termvis mcp - run termvis as an MCP server\n\n")
		fmt.Fprintf(os.Stderr, "Usage:\n  termvis mcp [flags]\n\n")
		fmt.Fprintf(os.Stderr, "Flags:\n")
		flags.PrintDefaults()
		fmt.Fprintf(os.Stderr, "\nWith no flags, serves over stdio (for MCP clients that spawn this process directly).\n")
		fmt.Fprintf(os.Stderr, "With -http, serves the Streamable HTTP/SSE transport instead.\n\n")
		fmt.Fprintf(os.Stderr, "WARNING: open_session runs arbitrary shell commands. -http has no\n")
		fmt.Fprintf(os.Stderr, "built-in authentication — do not bind it to a public interface without\n")
		fmt.Fprintf(os.Stderr, "your own auth (reverse proxy, mTLS, etc.), or you've built an\n")
		fmt.Fprintf(os.Stderr, "unauthenticated remote code execution endpoint.\n")
	}

	_ = flags.Parse(args)

	// Ensure open termvis sessions (and their ttyd/chrome children) don't get
	// orphaned if the MCP host/server process is killed or restarted.
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sigChan
		closeAllSessions()
		os.Exit(0)
	}()
	defer closeAllSessions()

	if *httpAddr != "" {
		fmt.Fprintf(os.Stderr, "termvis mcp: listening on %s (Streamable HTTP/SSE transport)\n", *httpAddr)
		handler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server {
			return newMCPServer()
		}, nil)
		if err := http.ListenAndServe(*httpAddr, handler); err != nil {
			log.Fatal(err)
		}
		return
	}

	if err := newMCPServer().Run(context.Background(), &mcp.StdioTransport{}); err != nil {
		log.Fatal(err)
	}
}
