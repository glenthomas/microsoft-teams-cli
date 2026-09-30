// Package auth handles Microsoft Entra ID sign-in for the CLI using MSAL.
package auth

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/AzureAD/microsoft-authentication-library-for-go/apps/public"
	"github.com/pkg/browser"

	"github.com/glenthomas/microsoft-teams-cli/internal/config"
)

// Scopes are the delegated Microsoft Graph permissions requested by the CLI.
// MSAL automatically adds openid, profile and offline_access.
var Scopes = []string{
	"https://graph.microsoft.com/User.Read",
	"https://graph.microsoft.com/Team.ReadBasic.All",
	"https://graph.microsoft.com/Channel.ReadBasic.All",
	"https://graph.microsoft.com/ChannelMessage.Read.All",
}

// ErrNotLoggedIn indicates there is no usable cached sign-in.
var ErrNotLoggedIn = errors.New("not logged in: run `teams login`")

// Account describes the signed-in user.
type Account struct {
	Username      string `json:"username"`
	HomeAccountID string `json:"homeAccountId"`
	TenantID      string `json:"tenantId,omitempty"`
}

// Authenticator acquires Graph access tokens.
type Authenticator struct {
	ClientID string
	Tenant   string
	client   public.Client
}

// New creates an Authenticator whose token cache lives in configDir.
func New(clientID, tenant, configDir string) (*Authenticator, error) {
	authority := "https://login.microsoftonline.com/" + tenant
	c, err := public.New(clientID,
		public.WithAuthority(authority),
		public.WithCache(&fileCache{path: config.CachePath(configDir)}),
	)
	if err != nil {
		return nil, fmt.Errorf("initialising authentication: %w", err)
	}
	return &Authenticator{ClientID: clientID, Tenant: tenant, client: c}, nil
}

// LoginOptions control the interactive sign-in flow.
type LoginOptions struct {
	// DeviceCode uses the device code flow instead of opening a browser.
	DeviceCode bool
	// LoginHint pre-fills the username on the sign-in page.
	LoginHint string
	// Status receives human-readable progress messages (e.g. stderr).
	Status io.Writer
}

// Login signs the user in, by default by opening the system browser and
// listening on a localhost redirect (auth code flow with PKCE).
func (a *Authenticator) Login(ctx context.Context, opts LoginOptions) (Account, error) {
	status := opts.Status
	if status == nil {
		status = io.Discard
	}
	var (
		res public.AuthResult
		err error
	)
	if opts.DeviceCode {
		var dc public.DeviceCode
		dc, err = a.client.AcquireTokenByDeviceCode(ctx, Scopes)
		if err != nil {
			return Account{}, fmt.Errorf("starting device code sign-in: %w", err)
		}
		fmt.Fprintln(status, dc.Result.Message)
		res, err = dc.AuthenticationResult(ctx)
	} else {
		iopts := []public.AcquireInteractiveOption{
			public.WithOpenURL(func(u string) error {
				fmt.Fprintln(status, "Opening your browser to sign in to Microsoft Teams...")
				fmt.Fprintf(status, "If the browser does not open, visit this URL:\n\n%s\n\n", u)
				if err := openBrowser(u); err != nil {
					fmt.Fprintf(status, "Could not open a browser automatically (%v).\n", err)
				}
				return nil
			}),
		}
		if opts.LoginHint != "" {
			iopts = append(iopts, public.WithLoginHint(opts.LoginHint))
		}
		res, err = a.client.AcquireTokenInteractive(ctx, Scopes, iopts...)
	}
	if err != nil {
		return Account{}, fmt.Errorf("sign-in failed: %w", err)
	}

	// Keep only the account that just signed in so later commands are
	// unambiguous.
	if accts, err := a.client.Accounts(ctx); err == nil {
		for _, acct := range accts {
			if acct.HomeAccountID != res.Account.HomeAccountID {
				_ = a.client.RemoveAccount(ctx, acct)
			}
		}
	}
	return toAccount(res.Account), nil
}

var openBrowser = func(u string) error {
	browser.Stdout = io.Discard
	browser.Stderr = io.Discard
	return browser.OpenURL(u)
}

// CurrentAccount returns the cached signed-in account.
func (a *Authenticator) CurrentAccount(ctx context.Context) (public.Account, error) {
	accts, err := a.client.Accounts(ctx)
	if err != nil {
		return public.Account{}, fmt.Errorf("reading token cache: %w", err)
	}
	if len(accts) == 0 {
		return public.Account{}, ErrNotLoggedIn
	}
	return accts[0], nil
}

// Token returns a valid Graph access token, refreshing it silently if needed.
func (a *Authenticator) Token(ctx context.Context) (string, error) {
	acct, err := a.CurrentAccount(ctx)
	if err != nil {
		return "", err
	}
	res, err := a.client.AcquireTokenSilent(ctx, Scopes, public.WithSilentAccount(acct))
	if err != nil {
		return "", fmt.Errorf("%w (session expired or consent missing: %v)", ErrNotLoggedIn, err)
	}
	return res.AccessToken, nil
}

// Logout removes all cached accounts.
func (a *Authenticator) Logout(ctx context.Context) ([]Account, error) {
	accts, err := a.client.Accounts(ctx)
	if err != nil {
		return nil, err
	}
	var removed []Account
	for _, acct := range accts {
		if err := a.client.RemoveAccount(ctx, acct); err != nil {
			return removed, err
		}
		removed = append(removed, toAccount(acct))
	}
	return removed, nil
}

func toAccount(a public.Account) Account {
	return Account{Username: a.PreferredUsername, HomeAccountID: a.HomeAccountID, TenantID: a.Realm}
}

// StaticToken is a TokenSource that returns a fixed access token, e.g. from
// the TEAMS_CLI_ACCESS_TOKEN environment variable.
type StaticToken string

// Token implements the graph TokenSource interface.
func (s StaticToken) Token(context.Context) (string, error) { return string(s), nil }

// EnvToken returns the access token supplied via environment, if any.
func EnvToken() (StaticToken, bool) {
	t := strings.TrimSpace(os.Getenv(config.EnvAccessToken))
	return StaticToken(t), t != ""
}
