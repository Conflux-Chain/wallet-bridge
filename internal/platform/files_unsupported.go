//go:build !windows && !darwin

package platform

import (
	"errors"
	"os"
)

var errUnsupported = errors.New("private credential files require Windows or macOS")

func dataHome() (string, error)               { return "", errUnsupported }
func createDirectory(string) error            { return errUnsupported }
func createFile(string) (*os.File, error)     { return nil, errUnsupported }
func openFile(string, bool) (*os.File, error) { return nil, errUnsupported }
func checkHandle(*os.File, bool) error        { return errUnsupported }
func lockFile(*os.File) error                 { return errUnsupported }
func unlockFile(*os.File) error               { return errUnsupported }
