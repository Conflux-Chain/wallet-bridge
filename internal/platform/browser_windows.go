package platform

import "golang.org/x/sys/windows"

// OpenBrowser submits a URL to the user's default browser without a shell.
func OpenBrowser(url string) error {
	verb, _ := windows.UTF16PtrFromString("open")
	target, err := windows.UTF16PtrFromString(url)
	if err != nil {
		return err
	}
	return windows.ShellExecute(0, verb, target, nil, nil, windows.SW_SHOWNORMAL)
}
