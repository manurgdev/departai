// Package version exposes build/version information for departai. Values can be
// injected at build time via ldflags (GoReleaser sets these), and fall back to
// the Go build info embedded by `go install` / `go build` when they aren't.
package version

import (
	"fmt"
	"runtime"
	"runtime/debug"
	"strings"
)

// Build-time variables. Set with, e.g.:
//
//	go build -ldflags "-X github.com/manurgdev/departai/internal/version.Version=v0.1.0 \
//	  -X github.com/manurgdev/departai/internal/version.Commit=$(git rev-parse HEAD) \
//	  -X github.com/manurgdev/departai/internal/version.Date=$(date -u +%Y-%m-%dT%H:%M:%SZ)"
var (
	Version = ""
	Commit  = ""
	Date    = ""
)

// info resolves the effective version, commit, and date, preferring ldflags
// values and falling back to embedded build info, then to safe defaults.
func info() (version, commit, date string) {
	version, commit, date = Version, Commit, Date

	if bi, ok := debug.ReadBuildInfo(); ok {
		if version == "" && bi.Main.Version != "" && bi.Main.Version != "(devel)" {
			version = bi.Main.Version
		}
		for _, s := range bi.Settings {
			switch s.Key {
			case "vcs.revision":
				if commit == "" {
					commit = s.Value
				}
			case "vcs.time":
				if date == "" {
					date = s.Value
				}
			}
		}
	}

	return orDefault(version, "dev"), orDefault(commit, "none"), orDefault(date, "unknown")
}

func orDefault(v, def string) string {
	if strings.TrimSpace(v) == "" {
		return def
	}
	return v
}

// Short returns just the version string (e.g. "v0.1.0" or "dev").
func Short() string {
	v, _, _ := info()
	return v
}

// Summary returns the user-facing one-liner shown by `--version`: the semantic
// version plus os/arch (useful for support, not sensitive). Deliberately omits
// the commit hash, build date, and Go toolchain version — those are internal
// metadata with no value to an end user of closed-source software, and are
// available via `--version --verbose` for diagnostics.
func Summary() string {
	v, _, _ := info()
	return fmt.Sprintf("departai %s (%s/%s)", v, runtime.GOOS, runtime.GOARCH)
}

// Detailed returns the full multi-line build-info block for `--version --verbose`
// (support / diagnostics): version, commit, build date, Go toolchain, os/arch.
func Detailed() string {
	v, c, d := info()
	if len(c) > 12 {
		c = c[:12] // short commit hash
	}
	return fmt.Sprintf(
		"departai %s\n  commit:  %s\n  built:   %s\n  go:      %s\n  os/arch: %s/%s",
		v, c, d, runtime.Version(), runtime.GOOS, runtime.GOARCH,
	)
}
