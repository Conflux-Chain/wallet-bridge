//go:build !windows

package platform

import (
	"errors"
	"syscall"
)

func IsPortInUse(err error) bool { return errors.Is(err, syscall.EADDRINUSE) }

func IsConnectionRefused(err error) bool { return errors.Is(err, syscall.ECONNREFUSED) }
