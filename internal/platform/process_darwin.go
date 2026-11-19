package platform

import (
	"os/exec"
	"syscall"
)

func detachProcess(cmd *exec.Cmd) error {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	return nil
}
