package cli

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/spf13/cobra"

	"github.com/glenthomas/microsoft-teams-cli/internal/auth"
	"github.com/glenthomas/microsoft-teams-cli/internal/config"
)

func (a *App) loginCmd() *cobra.Command {
	var (
		deviceCode bool
		loginHint  string
		timeout    time.Duration
	)
	cmd := &cobra.Command{
		Use:   "login",
		Short: "Sign in to Microsoft Teams (opens a browser window)",
		Long: `Sign in with your work or school account. By default a browser window is
opened and the CLI listens on a localhost redirect to receive the result
(OAuth 2.0 authorization code flow with PKCE). Use --device-code on machines
without a browser.

Tokens are cached in the CLI config directory (owner-only permissions) and
refreshed automatically by later commands.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if timeout <= 0 {
				return newUsageError(fmt.Errorf("--timeout must be positive"))
			}
			au, dir, err := a.authenticator()
			if err != nil {
				return err
			}
			ctx, cancel := context.WithTimeout(cmd.Context(), timeout)
			defer cancel()
			acct, err := au.Login(ctx, auth.LoginOptions{DeviceCode: deviceCode, LoginHint: loginHint, Status: a.Stderr})
			if err != nil {
				return err
			}
			if err := config.Save(dir, config.Settings{ClientID: au.ClientID, Tenant: au.Tenant}); err != nil {
				return fmt.Errorf("saving settings: %w", err)
			}
			return a.emit(map[string]any{"status": "logged_in", "account": acct}, func(w io.Writer) {
				fmt.Fprintf(w, "Logged in as %s\n", acct.Username)
			})
		},
	}
	cmd.Flags().BoolVar(&deviceCode, "device-code", false, "use the device code flow instead of opening a browser")
	cmd.Flags().StringVar(&loginHint, "login-hint", "", "username to pre-fill on the sign-in page")
	cmd.Flags().DurationVar(&timeout, "timeout", 5*time.Minute, "maximum time to wait for sign-in to complete")
	return cmd
}

func (a *App) logoutCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "logout",
		Short: "Remove cached credentials",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			au, dir, err := a.authenticator()
			if err != nil {
				return err
			}
			removed, err := au.Logout(cmd.Context())
			if err != nil {
				return err
			}
			if err := config.Remove(dir); err != nil {
				return err
			}
			if removed == nil {
				removed = []auth.Account{}
			}
			return a.emit(map[string]any{"status": "logged_out", "accounts": removed}, func(w io.Writer) {
				fmt.Fprintln(w, "Logged out")
			})
		},
	}
}

func (a *App) whoamiCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "whoami",
		Short: "Show the signed-in user",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			g, err := a.NewGraph(cmd.Context())
			if err != nil {
				return err
			}
			me, err := g.Me(cmd.Context())
			if err != nil {
				return err
			}
			return a.emit(me, func(w io.Writer) {
				fmt.Fprintf(w, "%s <%s>\n", me.DisplayName, me.UserPrincipalName)
			})
		},
	}
}
