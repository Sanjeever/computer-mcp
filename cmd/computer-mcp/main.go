package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/Sanjeever/computer-mcp/internal/i18n"
	"github.com/Sanjeever/computer-mcp/internal/server"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

var version = "dev"

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func run() error {
	flags := flag.NewFlagSet("computer-mcp", flag.ContinueOnError)
	lang := flags.String("lang", os.Getenv("COMPUTER_MCP_LANG"), "Language / 语言: en, zh-CN (default en)")
	showVersion := flags.Bool("version", false, "Print version / 显示版本")
	if err := flags.Parse(os.Args[1:]); err != nil {
		if err == flag.ErrHelp {
			return nil
		}
		return err
	}
	if flags.NArg() > 0 {
		return fmt.Errorf("unexpected arguments: %v", flags.Args())
	}
	if *showVersion {
		fmt.Println(version)
		return nil
	}
	catalog, err := i18n.Load(*lang)
	if err != nil {
		return err
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	engine := server.NewEngine(ctx)
	defer engine.Close()
	err = server.New(version, catalog, engine).Run(ctx, &mcp.StdioTransport{})
	if ctx.Err() != nil {
		return nil
	}
	return err
}
