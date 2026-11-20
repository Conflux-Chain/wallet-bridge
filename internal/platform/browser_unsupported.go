//go:build !windows && !darwin

package platform

func OpenBrowser(string) error { return errUnsupported }
