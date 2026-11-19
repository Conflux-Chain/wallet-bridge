package platform

import (
	"encoding/binary"
	"fmt"
	"os"
	"runtime"
	"syscall"
	"unsafe"

	"golang.org/x/sys/unix"
)

func dataHome() (string, error) { return os.UserConfigDir() }

func createDirectory(path string) error { return os.Mkdir(path, 0700) }

func createFile(path string) (*os.File, error) {
	fd, err := unix.Open(path, unix.O_RDWR|unix.O_CREAT|unix.O_EXCL|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0600)
	if err != nil {
		return nil, &os.PathError{Op: "create", Path: path, Err: err}
	}
	return os.NewFile(uintptr(fd), path), nil
}

func openFile(path string, directory bool) (*os.File, error) {
	flags := unix.O_RDONLY | unix.O_NOFOLLOW | unix.O_CLOEXEC | unix.O_NONBLOCK
	if directory {
		flags |= unix.O_DIRECTORY
	}
	fd, err := unix.Open(path, flags, 0)
	if err != nil {
		return nil, &os.PathError{Op: "open", Path: path, Err: err}
	}
	return os.NewFile(uintptr(fd), path), nil
}

func checkHandle(f *os.File, directory bool) error {
	info, err := f.Stat()
	if err != nil {
		return err
	}
	mode := os.FileMode(0600)
	if directory {
		mode = 0700
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat.Uid != uint32(os.Geteuid()) || info.Mode().Perm() != mode ||
		info.IsDir() != directory || (!directory && !info.Mode().IsRegular()) ||
		info.Mode()&(os.ModeSetuid|os.ModeSetgid|os.ModeSticky) != 0 {
		return fmt.Errorf("%s: %w", f.Name(), ErrInsecurePath)
	}
	return checkNoACL(f)
}

func checkNoACL(f *os.File) error {
	// fgetattrlist returns a length and an attrreference. A zero-length extended
	// security attribute means no ACL; reject any ACL rather than infer its rights.
	// ABI: apple-oss-distributions/xnu bsd/sys/attr.h and bsd/vfs/vfs_attrlist.c.
	attrs := unix.Attrlist{Bitmapcount: unix.ATTR_BIT_MAP_COUNT, Commonattr: unix.ATTR_CMN_EXTENDED_SECURITY}
	var buf [12]byte
	_, _, errno := unix.Syscall6(unix.SYS_FGETATTRLIST, f.Fd(), uintptr(unsafe.Pointer(&attrs)),
		uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)), unix.FSOPT_REPORT_FULLSIZE, 0)
	runtime.KeepAlive(f)
	if errno != 0 {
		return fmt.Errorf("check ACL on %s: %w", f.Name(), errno)
	}
	if binary.LittleEndian.Uint32(buf[:4]) != uint32(len(buf)) || binary.LittleEndian.Uint32(buf[8:]) != 0 {
		return fmt.Errorf("%s: %w (extended ACL)", f.Name(), ErrInsecurePath)
	}
	return nil
}
