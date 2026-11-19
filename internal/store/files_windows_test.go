package store

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/Conflux-Chain/wallet-bridge/internal/platform"
	"golang.org/x/sys/windows"
)

func TestUnsafeDatabaseACLRejected(t *testing.T) {
	for _, name := range []string{DatabaseName, DatabaseName + "-journal"} {
		t.Run(name, func(t *testing.T) {
			dir := filepath.Join(t.TempDir(), "data")
			s := openTestStore(t, dir)
			s.Close()
			path := filepath.Join(dir, name)
			sd, err := windows.SecurityDescriptorFromString("D:P(A;;FA;;;WD)")
			if err != nil {
				t.Fatal(err)
			}
			acl, _, err := sd.DACL()
			if err != nil {
				t.Fatal(err)
			}
			if err := windows.SetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION, nil, nil, acl, nil); err != nil {
				t.Fatal(err)
			}
			before, err := windows.GetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION)
			if err != nil {
				t.Fatal(err)
			}
			if s, err := Open(context.Background(), dir); !errors.Is(err, platform.ErrInsecurePath) {
				if s != nil {
					s.Close()
				}
				t.Fatal("unsafe database ACL accepted", err)
			}
			after, err := windows.GetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION)
			if err != nil || after.String() != before.String() {
				t.Fatal("unsafe database ACL was silently changed", err)
			}
		})
	}
}
