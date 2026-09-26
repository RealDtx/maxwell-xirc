// Package fscheck probes directory permissions and turns permission errors
// into messages that say who owns what and who xirc runs as.
package fscheck

import (
	"errors"
	"fmt"
	"os"
	"os/user"
	"strconv"
	"syscall"
)

type DirStatus struct {
	Path   string `json:"path"`
	Exists bool   `json:"exists"`
	Read   bool   `json:"read"`
	Write  bool   `json:"write"`
	Reason string `json:"reason,omitempty"`
}

func IsPermission(err error) bool {
	return errors.Is(err, os.ErrPermission) || errors.Is(err, syscall.EROFS)
}

func name(id uint32, lookup func(string) (string, error)) string {
	s := strconv.FormatUint(uint64(id), 10)
	if n, err := lookup(s); err == nil {
		return n
	}
	return s
}

// owner describes path's owner and mode, e.g. "owner root:root 0755".
func owner(path string) string {
	info, err := os.Stat(path)
	if err != nil {
		return "unreadable"
	}
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return fmt.Sprintf("mode %04o", info.Mode().Perm())
	}
	u := name(st.Uid, func(s string) (string, error) {
		x, err := user.LookupId(s)
		if err != nil {
			return "", err
		}
		return x.Username, nil
	})
	g := name(st.Gid, func(s string) (string, error) {
		x, err := user.LookupGroupId(s)
		if err != nil {
			return "", err
		}
		return x.Name, nil
	})
	return fmt.Sprintf("owner %s:%s %04o", u, g, info.Mode().Perm())
}

func self() string {
	return fmt.Sprintf("xirc runs as uid %d gid %d", os.Geteuid(), os.Getegid())
}

func Probe(path string) DirStatus {
	s := DirStatus{Path: path}
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			s.Reason = "does not exist: " + path
		} else {
			s.Reason = Describe(err, path).Error()
		}
		return s
	}
	if !info.IsDir() {
		s.Exists = true
		s.Reason = "not a directory: " + path
		return s
	}
	s.Exists = true
	if _, err := os.ReadDir(path); err == nil {
		s.Read = true
	}
	if f, err := os.CreateTemp(path, ".xirc_write_check_*"); err == nil {
		f.Close()
		os.Remove(f.Name())
		s.Write = true
	}
	switch {
	case !s.Read:
		s.Reason = fmt.Sprintf("not readable: %s (%s; %s)", path, owner(path), self())
	case !s.Write:
		s.Reason = fmt.Sprintf("not writable: %s (%s; %s)", path, owner(path), self())
	}
	return s
}

type permError struct {
	msg string
	err error
}

func (e *permError) Error() string { return e.msg }
func (e *permError) Unwrap() error { return e.err }

// Describe wraps permission errors with the directory's owner/mode and the
// process identity. dir is the directory the operation targeted.
func Describe(err error, dir string) error {
	if err == nil || !IsPermission(err) {
		return err
	}
	if errors.Is(err, syscall.EROFS) {
		return &permError{fmt.Sprintf("permission denied: %s is on a read-only filesystem", dir), err}
	}
	return &permError{fmt.Sprintf("permission denied: %s (%s; %s)", dir, owner(dir), self()), err}
}
