package platform

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/windows"
)

func TestBroadWindowsACLRejected(t *testing.T) {
	for _, directory := range []bool{false, true} {
		t.Run(map[bool]string{true: "directory", false: "file"}[directory], func(t *testing.T) {
			root := filepath.Join(t.TempDir(), "data")
			if err := EnsurePrivateDir(root); err != nil {
				t.Fatal(err)
			}
			path := root
			if !directory {
				path = filepath.Join(root, "key")
				f, err := CreatePrivateFile(path)
				if err != nil {
					t.Fatal(err)
				}
				f.Close()
			}
			user, err := currentSID()
			if err != nil {
				t.Fatal(err)
			}
			sd, err := windows.SecurityDescriptorFromString(fmt.Sprintf("D:P(A;;FA;;;%s)(A;;FA;;;WD)", user))
			if err != nil {
				t.Fatal(err)
			}
			acl, _, err := sd.DACL()
			if err != nil {
				t.Fatal(err)
			}
			if err := windows.SetNamedSecurityInfo(path, windows.SE_FILE_OBJECT,
				windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION, nil, nil, acl, nil); err != nil {
				t.Fatal(err)
			}
			before, err := windows.GetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION)
			if err != nil {
				t.Fatal(err)
			}
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
				t.Fatalf("broad ACL accepted: %v", err)
			}
			after, err := windows.GetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION)
			if err != nil || after.String() != before.String() {
				t.Fatal("unsafe existing ACL was silently changed")
			}
		})
	}
}
