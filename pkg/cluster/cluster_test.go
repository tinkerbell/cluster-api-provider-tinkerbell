package cluster

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func validKubeconfig() []byte {
	return []byte(`apiVersion: v1
kind: Config
clusters:
- cluster:
    server: https://127.0.0.1:6443
  name: test
contexts:
- context:
    cluster: test
    user: test
  name: test
current-context: test
users:
- name: test
  user:
    token: fake-token
`)
}

func writeFile(t *testing.T, name string, data []byte) string {
	t.Helper()

	p := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(p, data, 0o600); err != nil {
		t.Fatal(err)
	}

	return p
}

func TestRestConfig_NoConfigError(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		path func(t *testing.T) string
	}{
		"empty path": {
			path: func(_ *testing.T) string { return "" },
		},
		"missing file": {
			path: func(t *testing.T) string {
				t.Helper()
				return filepath.Join(t.TempDir(), "does-not-exist")
			},
		},
		"empty file": {
			path: func(t *testing.T) string {
				t.Helper()
				return writeFile(t, "empty", nil)
			},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			cfg, err := RestConfig(tc.path(t))
			if !errors.Is(err, NoConfigError{}) {
				t.Fatalf("expected NoConfigError, got %v", err)
			}
			if cfg != nil {
				t.Fatal("expected nil config")
			}
		})
	}
}

func TestRestConfig_Errors(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		path func(t *testing.T) string
	}{
		"invalid kubeconfig": {
			path: func(t *testing.T) string {
				t.Helper()
				return writeFile(t, "bad", []byte("not-yaml{{{"))
			},
		},
		"unreadable file": {
			path: func(t *testing.T) string {
				t.Helper()

				p := writeFile(t, "noperm", validKubeconfig())
				if err := os.Chmod(p, 0o000); err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = os.Chmod(p, 0o600) })

				return p
			},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			cfg, err := RestConfig(tc.path(t))
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if errors.Is(err, NoConfigError{}) {
				t.Fatal("expected non-NoConfigError, got NoConfigError")
			}
			if cfg != nil {
				t.Fatal("expected nil config")
			}
		})
	}
}

func TestRestConfig_ValidKubeconfig(t *testing.T) {
	t.Parallel()

	p := writeFile(t, "valid", validKubeconfig())

	cfg, err := RestConfig(p)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	wantHost := "https://127.0.0.1:6443"
	if cfg.Host != wantHost {
		t.Fatalf("expected host %q, got %q", wantHost, cfg.Host)
	}
}
