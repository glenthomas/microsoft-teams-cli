// Package cli defines the `teams` command-line interface.
package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/glenthomas/microsoft-teams-cli/internal/auth"
	"github.com/glenthomas/microsoft-teams-cli/internal/config"
	"github.com/glenthomas/microsoft-teams-cli/internal/graph"
	"github.com/glenthomas/microsoft-teams-cli/internal/teams"
)

// Version is set at build time via -ldflags "-X .../internal/cli.Version=...".
var Version = "dev"

// App holds CLI state shared by commands.
type App struct {
	Stdout io.Writer
	Stderr io.Writer
	Now    func() time.Time
	// NewGraph builds the Graph client; overridable in tests.
	NewGraph func(ctx context.Context) (*graph.Client, error)

	format   string
	clientID string
	tenant   string
}

// Main runs the CLI with os.Args and returns the process exit code.
func Main(ctx context.Context) int {
	app := &App{Stdout: os.Stdout, Stderr: os.Stderr, Now: time.Now}
	return app.Run(ctx, os.Args[1:])
}

// Run executes the CLI with args and returns the process exit code.
func (a *App) Run(ctx context.Context, args []string) int {
	if a.Now == nil {
		a.Now = time.Now
	}
	if a.NewGraph == nil {
		a.NewGraph = a.defaultGraph
	}
	root := a.rootCmd()
	root.SetArgs(args)
	root.SetOut(a.Stdout)
	root.SetErr(a.Stderr)
	err := root.ExecuteContext(ctx)
	if err == nil {
		return ExitOK
	}
	info, code := classify(err)
	enc := json.NewEncoder(a.Stderr)
	enc.SetIndent("", "  ")
	_ = enc.Encode(map[string]any{"error": info})
	return code
}

func (a *App) rootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:   "teams",
		Short: "Read Microsoft Teams data from the command line",
		Long: `teams is a command-line interface for reading Microsoft Teams data via
Microsoft Graph. It is designed to be invoked by AI coding agents and scripts:
results are written to stdout as JSON, errors are written to stderr as JSON
({"error": {"code": ..., "message": ...}}) and exit codes are stable:

  0  success
  1  unexpected error
  2  usage error (bad flags/arguments)
  3  not logged in / session expired (run "teams login")
  4  team, channel or message not found, or name is ambiguous
  5  Microsoft Graph API error (e.g. forbidden, throttled)

Authenticate once with "teams login", which opens a browser window.`,
		Example: `  teams login
  teams search --channel platform-engineering --since 30d --query "private endpoints"
  teams messages --team "Platform" --channel general --since 7d
  teams thread --channel platform-engineering --id 1717171717171`,
		Version:       Version,
		SilenceErrors: true,
		SilenceUsage:  true,
	}
	root.SetFlagErrorFunc(func(_ *cobra.Command, err error) error { return newUsageError(err) })
	pf := root.PersistentFlags()
	pf.StringVarP(&a.format, "format", "f", "json", "output format: json or text")
	pf.StringVar(&a.clientID, "client-id", "", "Entra ID application (client) ID [env "+config.EnvClientID+"]")
	pf.StringVar(&a.tenant, "tenant", "", "Entra ID tenant ID or domain [env "+config.EnvTenant+"] (default \""+config.DefaultTenant+"\")")
	root.PersistentPreRunE = func(*cobra.Command, []string) error {
		if a.format != "json" && a.format != "text" {
			return newUsageError(fmt.Errorf("invalid --format %q: must be json or text", a.format))
		}
		return nil
	}

	root.AddCommand(
		a.loginCmd(),
		a.logoutCmd(),
		a.whoamiCmd(),
		a.teamsCmd(),
		a.channelsCmd(),
		a.messagesCmd(),
		a.searchCmd(),
		a.threadCmd(),
	)
	return root
}

// authSettings returns the effective client ID and tenant.
func (a *App) authSettings() (dir, clientID, tenant string, err error) {
	dir, err = config.Dir()
	if err != nil {
		return "", "", "", err
	}
	saved, err := config.Load(dir)
	if err != nil {
		return "", "", "", err
	}
	clientID = config.Resolve(a.clientID, config.EnvClientID, saved.ClientID, config.DefaultClientID)
	tenant = config.Resolve(a.tenant, config.EnvTenant, saved.Tenant, config.DefaultTenant)
	return dir, clientID, tenant, nil
}

func (a *App) authenticator() (*auth.Authenticator, string, error) {
	dir, clientID, tenant, err := a.authSettings()
	if err != nil {
		return nil, "", err
	}
	au, err := auth.New(clientID, tenant, dir)
	return au, dir, err
}

func (a *App) defaultGraph(context.Context) (*graph.Client, error) {
	base := os.Getenv(config.EnvGraphURL)
	if base == "" {
		base = config.DefaultGraphURL
	}
	if tok, ok := auth.EnvToken(); ok {
		return graph.NewClient(base, tok), nil
	}
	au, _, err := a.authenticator()
	if err != nil {
		return nil, err
	}
	return graph.NewClient(base, au), nil
}

func (a *App) service(ctx context.Context) (*teams.Service, error) {
	g, err := a.NewGraph(ctx)
	if err != nil {
		return nil, err
	}
	return &teams.Service{Graph: g}, nil
}

// emit writes v to stdout as JSON, or via textFn when --format text.
func (a *App) emit(v any, textFn func(w io.Writer)) error {
	if a.format == "text" && textFn != nil {
		textFn(a.Stdout)
		return nil
	}
	enc := json.NewEncoder(a.Stdout)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	return enc.Encode(v)
}
