// Package goenv builds the environment the go command runs in when it is
// pointed at a repository someone else wrote.
//
// Inheriting the caller's environment is how analysis breaks or becomes
// unsafe: a global GOFLAGS=-mod=vendor fails every repository without a vendor
// directory, GOFLAGS=-toolexec runs an arbitrary program for every compile, a
// go.mod toolchain line makes GOTOOLCHAIN=auto download and execute a different
// Go, and cgo runs the C compiler with flags the repository chose. Each of
// those is decided here instead of inherited.
package goenv

import (
	"os"
	"path/filepath"
	"strings"
)

type Options struct {
	// CGO keeps cgo enabled. Off by default: with it off, cgo files are left
	// out of the build, which costs precision but runs no C compiler.
	CGO bool
	// Offline forbids module downloads; only the module cache is used.
	Offline bool
	// Extra is appended after everything else, for settings a caller owns
	// (GOPRIVATE, GOOS, ...). Keys this package controls are refused.
	Extra []string
}

var controlled = map[string]bool{
	"GOFLAGS":     true,
	"GOTOOLCHAIN": true,
	"CGO_ENABLED": true,
	"GOWORK":      true,
}

// For returns base with the controlled keys replaced, for running the go
// command in dir.
func For(dir string, base []string, opt Options) []string {
	out := make([]string, 0, len(base)+6)
	for _, kv := range base {
		key, _, _ := strings.Cut(kv, "=")
		if controlled[key] || (opt.Offline && key == "GOPROXY") {
			continue
		}
		out = append(out, kv)
	}

	out = append(out,
		"GOFLAGS="+ModFlag(dir),
		"GOTOOLCHAIN=local",
		"GOWORK="+Workspace(dir),
	)
	if opt.CGO {
		out = append(out, "CGO_ENABLED=1")
	} else {
		out = append(out, "CGO_ENABLED=0")
	}
	if opt.Offline {
		out = append(out, "GOPROXY=off")
	}
	for _, kv := range opt.Extra {
		key, _, _ := strings.Cut(kv, "=")
		if controlled[key] {
			continue
		}
		out = append(out, kv)
	}
	return out
}

// Default is For with the current process environment.
func Default(dir string, opt Options) []string { return For(dir, os.Environ(), opt) }

// ModFlag uses the repository's own vendor directory when it has one, and
// otherwise refuses to rewrite its go.mod: an analysis that edits go.mod has
// already changed the code it was asked to read.
func ModFlag(dir string) string {
	if root, ok := moduleRoot(dir); ok {
		if _, err := os.Stat(filepath.Join(root, "vendor", "modules.txt")); err == nil {
			return "-mod=vendor"
		}
	}
	return "-mod=readonly"
}

// Workspace honours a go.work committed at the module root and ignores one
// found further up, which belongs to whoever owns the parent directory.
func Workspace(dir string) string {
	if root, ok := moduleRoot(dir); ok {
		work := filepath.Join(root, "go.work")
		if _, err := os.Stat(work); err == nil {
			return work
		}
	}
	return "off"
}

func moduleRoot(dir string) (string, bool) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return "", false
	}
	for d := abs; ; d = filepath.Dir(d) {
		if _, err := os.Stat(filepath.Join(d, "go.mod")); err == nil {
			return d, true
		}
		if filepath.Dir(d) == d {
			return "", false
		}
	}
}

// Get returns the last value of key in env, which is the one exec uses.
func Get(env []string, key string) (string, bool) {
	val, found := "", false
	for _, kv := range env {
		k, v, ok := strings.Cut(kv, "=")
		if ok && k == key {
			val, found = v, true
		}
	}
	return val, found
}
