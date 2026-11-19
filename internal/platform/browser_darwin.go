package platform

import "os/exec"

// OpenBrowser submits a URL to the user's default browser without a shell.
func OpenBrowser(url string) error {
	return exec.Command("/usr/bin/open", url).Run()
}
