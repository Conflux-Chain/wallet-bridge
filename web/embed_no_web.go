//go:build no_web

package web

import "io/fs"

type emptyAssets struct{}

func (emptyAssets) Open(name string) (fs.File, error) {
	return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrNotExist}
}

func Assets() fs.FS {
	return emptyAssets{}
}
