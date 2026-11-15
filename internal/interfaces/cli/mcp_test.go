package cli_test

import (
	"context"
	"strings"
	"testing"

	"github.com/romero-project/romero/internal/interfaces/cli"
)

func TestMCPCmd_FlagsAndValidation(t *testing.T) {
	var got cli.MCPServeOptions
	cmd := cli.NewRootCmd(cli.Dependencies{
		MCP: func(_ context.Context, opts cli.MCPServeOptions) error {
			got = opts
			return nil
		},
	})
	cmd.SetArgs([]string{"mcp", "--dsn", "postgres://u:p@localhost/db", "--transport", "stdio", "--api-key", "romero_k1"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("stdio mcp cmd failed: %v", err)
	}
	if got.Transport != "stdio" || got.APIKey != "romero_k1" {
		t.Fatalf("unexpected opts: %+v", got)
	}

	// SSE transport does not require --api-key at startup
	cmdSSE := cli.NewRootCmd(cli.Dependencies{
		MCP: func(_ context.Context, opts cli.MCPServeOptions) error {
			got = opts
			return nil
		},
	})
	cmdSSE.SetArgs([]string{"mcp", "--dsn", "postgres://u:p@localhost/db", "--transport", "sse", "--listen", "127.0.0.1:9421"})
	if err := cmdSSE.Execute(); err != nil {
		t.Fatalf("sse mcp cmd failed: %v", err)
	}
	if got.Transport != "sse" || got.ListenAddr != "127.0.0.1:9421" {
		t.Fatalf("unexpected sse opts: %+v", got)
	}

	// Invalid transport rejected
	cmdBad := cli.NewRootCmd(cli.Dependencies{
		MCP: func(_ context.Context, _ cli.MCPServeOptions) error { return nil },
	})
	cmdBad.SetArgs([]string{"mcp", "--dsn", "postgres://u:p@localhost/db", "--transport", "grpc"})
	if err := cmdBad.Execute(); err == nil {
		t.Fatal("expected error for unsupported transport")
	}

	// Stdio without API key rejected
	t.Setenv("ROMERO_API_KEY", "")
	cmdNoKey := cli.NewRootCmd(cli.Dependencies{
		MCP: func(_ context.Context, _ cli.MCPServeOptions) error { return nil },
	})
	cmdNoKey.SetArgs([]string{"mcp", "--dsn", "postgres://u:p@localhost/db", "--transport", "stdio"})
	err := cmdNoKey.Execute()
	if err == nil || !strings.Contains(err.Error(), "api key") {
		t.Fatalf("expected missing api key error, got %v", err)
	}
}

func TestServeCmd_MCPListenResolution(t *testing.T) {
	var gotREST, gotMCP string
	cmd := cli.NewRootCmd(cli.Dependencies{
		ServeWithMCP: func(_ context.Context, _ string, restAddr, mcpAddr string) error {
			gotREST = restAddr
			gotMCP = mcpAddr
			return nil
		},
	})
	cmd.SetArgs([]string{"serve", "--dsn", "postgres://u:p@localhost/db", "--listen", "127.0.0.1:8420", "--mcp-listen", "127.0.0.1:9422"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("serve with --mcp-listen failed: %v", err)
	}
	if gotREST != "127.0.0.1:8420" || gotMCP != "127.0.0.1:9422" {
		t.Fatalf("unexpected listen addrs: rest=%q mcp=%q", gotREST, gotMCP)
	}
}
