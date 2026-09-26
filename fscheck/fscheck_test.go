package fscheck

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

func skipIfRoot(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("permission bits don't apply to root")
	}
}

func TestProbe(t *testing.T) {
	skipIfRoot(t)
	base := t.TempDir()
	rw := filepath.Join(base, "rw")
	ro := filepath.Join(base, "ro")
	none := filepath.Join(base, "none")
	os.Mkdir(rw, 0755)
	os.Mkdir(ro, 0555)
	os.Mkdir(none, 0000)
	t.Cleanup(func() { os.Chmod(ro, 0755); os.Chmod(none, 0755) })

	cases := []struct {
		path                string
		exists, read, write bool
	}{
		{rw, true, true, true},
		{ro, true, true, false},
		{none, true, false, false},
		{filepath.Join(base, "missing"), false, false, false},
	}
	for _, c := range cases {
		s := Probe(c.path)
		if s.Exists != c.exists || s.Read != c.read || s.Write != c.write {
			t.Errorf("%s: got %+v", filepath.Base(c.path), s)
		}
		if (!c.write) && s.Reason == "" {
			t.Errorf("%s: missing reason", filepath.Base(c.path))
		}
	}
	if entries, _ := os.ReadDir(rw); len(entries) != 0 {
		t.Error("probe file left behind")
	}
	if s := Probe(ro); !strings.Contains(s.Reason, "not writable") || !strings.Contains(s.Reason, "uid") {
		t.Errorf("reason lacks detail: %q", s.Reason)
	}
}

func TestDescribe(t *testing.T) {
	skipIfRoot(t)
	dir := t.TempDir()
	os.Chmod(dir, 0555)
	defer os.Chmod(dir, 0755)
	_, err := os.Create(filepath.Join(dir, "x"))
	d := Describe(err, dir)
	if !IsPermission(d) || !errors.Is(d, os.ErrPermission) {
		t.Errorf("Describe lost the permission error: %v", d)
	}
	if !strings.Contains(d.Error(), "permission denied: "+dir) {
		t.Errorf("message: %q", d.Error())
	}
	other := errors.New("boom")
	if Describe(other, dir) != other {
		t.Error("non-permission errors must pass through")
	}
	if !IsPermission(&os.PathError{Op: "open", Path: "/x", Err: syscall.EROFS}) {
		t.Error("EROFS should count as permission")
	}
}
