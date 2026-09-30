// Command teams is a command-line interface for Microsoft Teams.
package main

import (
	"context"
	"os"
	"os/signal"

	"github.com/glenthomas/microsoft-teams-cli/internal/cli"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	code := cli.Main(ctx)
	stop()
	os.Exit(code)
}
