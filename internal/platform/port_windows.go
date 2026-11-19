package platform

import (
	"errors"
	"syscall"

	"golang.org/x/sys/windows"
)

// IsPortInUse includes Windows exclusive-address conflicts and reserved ports,
// both of which can report WSAEACCES instead of WSAEADDRINUSE on bind.
func IsPortInUse(err error) bool {
	return errors.Is(err, syscall.EADDRINUSE) || errors.Is(err, windows.WSAEADDRINUSE) || errors.Is(err, windows.WSAEACCES)
}
