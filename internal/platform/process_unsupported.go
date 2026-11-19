//go:build !windows && !darwin

package platform

import "os/exec"

func detachProcess(*exec.Cmd) error { return errUnsupported }
