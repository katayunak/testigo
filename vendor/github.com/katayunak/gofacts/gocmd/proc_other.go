//go:build !unix

package gocmd

import "os/exec"

func killGroupOnCancel(cmd *exec.Cmd) {}
