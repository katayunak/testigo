// Package gocmd runs the go command against a repository: build, vet, test.
//
// It is not a sandbox. go test executes the repository's code with the
// caller's privileges, network and filesystem. What this package guarantees is
// narrower: the environment is the one goenv builds, the run has a deadline
// that kills the whole process group (test binaries included), and output is
// bounded. Whether to run untrusted code at all is the caller's decision.
package gocmd

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"time"

	"github.com/katayunak/gofacts/goenv"
)

type Options struct {
	Env       goenv.Options
	Timeout   time.Duration
	MaxOutput int
}

const (
	DefaultTimeout   = 5 * time.Minute
	DefaultMaxOutput = 1 << 20
)

type Result struct {
	Args      []string
	Output    string
	Truncated bool
	ExitCode  int
	TimedOut  bool
	Duration  time.Duration
}

func (r Result) OK() bool { return r.ExitCode == 0 && !r.TimedOut }

var ErrTimeout = errors.New("go command timed out")

// Run executes `go args...` in dir. A non-zero exit is reported in the Result,
// not as an error: a failing test is an answer. The error is for runs that
// produced no answer — the command could not start, or the deadline passed.
func Run(ctx context.Context, dir string, opt Options, args ...string) (Result, error) {
	timeout := opt.Timeout
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	limit := opt.MaxOutput
	if limit <= 0 {
		limit = DefaultMaxOutput
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "go", args...)
	cmd.Dir = dir
	cmd.Env = goenv.Default(dir, opt.Env)
	out := newBoundedBuffer(limit)
	cmd.Stdout, cmd.Stderr = out, out
	cmd.WaitDelay = 5 * time.Second
	killGroupOnCancel(cmd)

	start := time.Now()
	err := cmd.Run()
	res := Result{
		Args:      append([]string{"go"}, args...),
		Output:    out.String(),
		Truncated: out.Truncated(),
		Duration:  time.Since(start),
	}
	if ctx.Err() != nil {
		res.TimedOut, res.ExitCode = true, -1
		return res, fmt.Errorf("%v after %s: %w", res.Args, timeout, ErrTimeout)
	}
	var exit *exec.ExitError
	switch {
	case err == nil:
	case errors.As(err, &exit):
		res.ExitCode = exit.ExitCode()
	default:
		return res, fmt.Errorf("%v: %w", res.Args, err)
	}
	return res, nil
}
