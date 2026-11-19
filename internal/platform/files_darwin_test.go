package platform

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestBroadMacPermissionsRejected(t *testing.T) {
	for _, directory := range []bool{false, true} {
		t.Run(map[bool]string{true: "directory", false: "file"}[directory], func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "data")
			if err := EnsurePrivateDir(path); err != nil {
				t.Fatal(err)
			}
			mode := os.FileMode(0755)
			if !directory {
				path = filepath.Join(path, "key")
				f, err := CreatePrivateFile(path)
				if err != nil {
					t.Fatal(err)
				}
				f.Close()
				mode = 0644
			}
			if err := os.Chmod(path, mode); err != nil {
				t.Fatal(err)
			}
			var err error
			if directory {
				err = EnsurePrivateDir(path)
			} else {
				var f *os.File
				f, err = OpenPrivateFile(path)
				if f != nil {
					f.Close()
				}
			}
			if !errors.Is(err, ErrInsecurePath) {
				t.Fatalf("broad permissions accepted: %v", err)
			}
			info, err := os.Stat(path)
			if err != nil || info.Mode().Perm() != mode {
				t.Fatal("unsafe existing permissions were silently changed")
			}
		})
	}
}

func TestMacExtendedACLRejected(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "data")
	if err := EnsurePrivateDir(dir); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "key")
	f, err := CreatePrivateFile(path)
	if err != nil {
		t.Fatal(err)
	}
	f.Close()
	if output, err := exec.Command("/bin/chmod", "+a", "everyone allow read", path).CombinedOutput(); err != nil {
		t.Fatalf("set test ACL: %v: %s", err, output)
	}
	if f, err := OpenPrivateFile(path); !errors.Is(err, ErrInsecurePath) {
		if f != nil {
			f.Close()
		}
		t.Fatalf("extended ACL accepted: %v", err)
	}
}
