package auth

import (
	"context"
	"errors"
	"os"

	"github.com/AzureAD/microsoft-authentication-library-for-go/apps/cache"

	"github.com/glenthomas/microsoft-teams-cli/internal/config"
)

// fileCache persists the MSAL token cache to a single owner-only file.
type fileCache struct {
	path string
}

func (f *fileCache) Replace(ctx context.Context, c cache.Unmarshaler, _ cache.ReplaceHints) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	b, err := os.ReadFile(f.path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	return c.Unmarshal(b)
}

func (f *fileCache) Export(ctx context.Context, c cache.Marshaler, _ cache.ExportHints) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	b, err := c.Marshal()
	if err != nil {
		return err
	}
	return config.WriteFileAtomic(f.path, b)
}
