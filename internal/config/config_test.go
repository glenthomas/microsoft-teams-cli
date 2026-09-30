package config

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestSaveLoadRemove(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "nested")
	if s, err := Load(dir); err != nil || s != (Settings{}) {
		t.Fatalf("Load missing = %+v, %v", s, err)
	}
	want := Settings{ClientID: "client", Tenant: "contoso.onmicrosoft.com"}
	if err := Save(dir, want); err != nil {
		t.Fatal(err)
	}
	if got, err := Load(dir); err != nil || got != want {
		t.Fatalf("Load = %+v, %v", got, err)
	}
	if runtime.GOOS != "windows" {
		fi, err := os.Stat(filepath.Join(dir, settingsFile))
		if err != nil || fi.Mode().Perm() != 0o600 {
			t.Fatalf("expected 0600 permissions, got %v %v", fi.Mode(), err)
		}
	}
	if err := Remove(dir); err != nil {
		t.Fatal(err)
	}
	if err := Remove(dir); err != nil {
		t.Fatalf("Remove should be idempotent: %v", err)
	}
}

func TestResolvePrecedence(t *testing.T) {
	t.Setenv(EnvTenant, "")
	if got := Resolve("", EnvTenant, "", "def"); got != "def" {
		t.Errorf("default: %s", got)
	}
	if got := Resolve("", EnvTenant, "saved", "def"); got != "saved" {
		t.Errorf("saved: %s", got)
	}
	t.Setenv(EnvTenant, "env")
	if got := Resolve("", EnvTenant, "saved", "def"); got != "env" {
		t.Errorf("env: %s", got)
	}
	if got := Resolve("flag", EnvTenant, "saved", "def"); got != "flag" {
		t.Errorf("flag: %s", got)
	}
}
