//go:build !no_web

package web

import (
	"io/fs"
	"regexp"
	"strings"
	"testing"
)

func TestEmbeddedAssets(t *testing.T) {
	assets := Assets()
	index, err := fs.ReadFile(assets, "index.html")
	if err != nil {
		t.Fatal(err)
	}
	links := regexp.MustCompile(`(?:src|href)="(/assets/[^"]+)"`).FindAllStringSubmatch(string(index), -1)
	if len(links) == 0 {
		t.Fatal("index.html has no bundled asset references")
	}
	for _, link := range links {
		name := strings.TrimPrefix(link[1], "/")
		entry, err := fs.Stat(assets, name)
		if err != nil {
			t.Errorf("index.html references missing asset %q: %v", name, err)
		} else if entry.IsDir() {
			t.Errorf("asset %q is a directory", name)
		}
	}
}
