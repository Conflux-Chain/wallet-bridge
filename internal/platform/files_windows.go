package platform

import (
	"fmt"
	"os"
	"unsafe"

	"golang.org/x/sys/windows"
)

func dataHome() (string, error) { return os.UserCacheDir() }

// CommitRecord publishes a synced temporary file in the same directory.
func CommitRecord(from, to string) error {
	source, err := windows.UTF16PtrFromString(from)
	if err != nil {
		return err
	}
	dest, err := windows.UTF16PtrFromString(to)
	if err != nil {
		return err
	}
	return windows.MoveFileEx(source, dest, windows.MOVEFILE_REPLACE_EXISTING|windows.MOVEFILE_WRITE_THROUGH)
}

func currentSID() (*windows.SID, error) {
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return nil, err
	}
	return user.User.Sid.Copy()
}

func privateAttributes(directory bool) (*windows.SecurityAttributes, error) {
	sid, err := currentSID()
	if err != nil {
		return nil, err
	}
	inherit := ""
	if directory {
		inherit = "OICI"
	}
	sd, err := windows.SecurityDescriptorFromString(fmt.Sprintf(
		"O:%sD:P(A;%s;FA;;;%s)(A;%s;FA;;;SY)", sid, inherit, sid, inherit))
	if err != nil {
		return nil, err
	}
	return &windows.SecurityAttributes{
		Length: uint32(unsafe.Sizeof(windows.SecurityAttributes{})), SecurityDescriptor: sd,
	}, nil
}

func createDirectory(path string) error {
	name, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return err
	}
	attrs, err := privateAttributes(true)
	if err == nil {
		err = windows.CreateDirectory(name, attrs)
	}
	if err != nil {
		return &os.PathError{Op: "mkdir", Path: path, Err: err}
	}
	return nil
}

func createFile(path string) (*os.File, error) {
	attrs, err := privateAttributes(false)
	if err != nil {
		return nil, err
	}
	return windowsFile(path, windows.GENERIC_READ|windows.GENERIC_WRITE, attrs, windows.CREATE_NEW, false)
}

func openFile(path string, directory bool) (*os.File, error) {
	return windowsFile(path, windows.GENERIC_READ, nil, windows.OPEN_EXISTING, directory)
}

func windowsFile(path string, access uint32, attrs *windows.SecurityAttributes, disposition uint32, directory bool) (*os.File, error) {
	name, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return nil, err
	}
	flags := uint32(windows.FILE_FLAG_OPEN_REPARSE_POINT)
	if directory {
		flags |= windows.FILE_FLAG_BACKUP_SEMANTICS
	}
	handle, err := windows.CreateFile(name, access,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
		attrs, disposition, flags, 0)
	if err != nil {
		return nil, &os.PathError{Op: "open", Path: path, Err: err}
	}
	return os.NewFile(uintptr(handle), path), nil
}

func checkHandle(f *os.File, mode os.FileMode) error {
	directory := mode.IsDir()
	handle := windows.Handle(f.Fd())
	var info windows.ByHandleFileInformation
	if err := windows.GetFileInformationByHandle(handle, &info); err != nil {
		return err
	}
	if info.FileAttributes&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 ||
		(info.FileAttributes&windows.FILE_ATTRIBUTE_DIRECTORY != 0) != directory {
		return fmt.Errorf("%s: %w (unexpected file type)", f.Name(), ErrInsecurePath)
	}
	sd, err := windows.GetSecurityInfo(handle, windows.SE_FILE_OBJECT,
		windows.OWNER_SECURITY_INFORMATION|windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		return err
	}
	if err := checkDescriptor(sd, directory); err != nil {
		return fmt.Errorf("%s: %w", f.Name(), err)
	}
	return nil
}

func checkDescriptor(sd *windows.SECURITY_DESCRIPTOR, directory bool) error {
	user, err := currentSID()
	if err != nil {
		return err
	}
	owner, _, err := sd.Owner()
	if err != nil {
		return err
	}
	control, _, err := sd.Control()
	if err != nil {
		return err
	}
	dacl, _, err := sd.DACL()
	if err != nil {
		return err
	}
	if owner == nil || !owner.Equals(user) || control&windows.SE_DACL_PROTECTED == 0 || dacl == nil || dacl.AceCount != 2 {
		return ErrInsecurePath
	}
	flags := uint8(0)
	if directory {
		flags = windows.OBJECT_INHERIT_ACE | windows.CONTAINER_INHERIT_ACE
	}
	seen := make(map[string]bool, 2)
	for i := uint32(0); i < uint32(dacl.AceCount); i++ {
		var ace *windows.ACCESS_ALLOWED_ACE
		if err := windows.GetAce(dacl, i, &ace); err != nil {
			return err
		}
		// FILE_ALL_ACCESS, as encoded by SDDL's FA right.
		if ace.Header.AceType != windows.ACCESS_ALLOWED_ACE_TYPE || ace.Header.AceFlags != flags || ace.Mask != 0x1f01ff {
			return ErrInsecurePath
		}
		sid := (*windows.SID)(unsafe.Pointer(&ace.SidStart)).String()
		if (sid != user.String() && sid != "S-1-5-18") || seen[sid] {
			return ErrInsecurePath
		}
		seen[sid] = true
	}
	return nil
}
