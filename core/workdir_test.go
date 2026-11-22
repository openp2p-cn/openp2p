package openp2p

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestWorkdirFromArgs(t *testing.T) {
	cases := []struct {
		args []string
		want string
	}{
		{nil, ""},
		{[]string{"-node", "x"}, ""},
		{[]string{"-workdir", "/tmp/a"}, "/tmp/a"},
		{[]string{"--workdir", "/tmp/b", "-node", "x"}, "/tmp/b"},
		{[]string{"-workdir=/tmp/c"}, "/tmp/c"},
		{[]string{"--workdir=/tmp/d"}, "/tmp/d"},
		{[]string{"-workdir"}, ""},
	}
	for _, tc := range cases {
		if got := workdirFromArgs(tc.args); got != tc.want {
			t.Errorf("workdirFromArgs(%q) = %q, want %q", tc.args, got, tc.want)
		}
	}
}

func TestResolveWorkdir(t *testing.T) {
	t.Setenv("OPENP2P_WORKDIR", "")
	if got := resolveWorkdir("/fallback", []string{"-workdir", "/flag"}); got != "/flag" {
		t.Fatalf("flag should win, got %q", got)
	}
	t.Setenv("OPENP2P_WORKDIR", "/env")
	if got := resolveWorkdir("/fallback", []string{"-node", "x"}); got != "/env" {
		t.Fatalf("env should win over fallback, got %q", got)
	}
	if got := resolveWorkdir("/fallback", []string{"-workdir", "/flag"}); got != "/flag" {
		t.Fatalf("flag should win over env, got %q", got)
	}
	t.Setenv("OPENP2P_WORKDIR", "")
	if got := resolveWorkdir("/fallback", nil); got != "/fallback" {
		t.Fatalf("fallback expected, got %q", got)
	}
}

func TestPrepareWorkdir(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "nested", "work")
	old, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chdir(old) }()
	if err := prepareWorkdir(dir); err != nil {
		t.Fatalf("prepareWorkdir: %v", err)
	}
	if got, _ := os.Getwd(); got != dir {
		t.Fatalf("cwd = %q, want %q", got, dir)
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(dir)
		if err != nil {
			t.Fatal(err)
		}
		if perm := info.Mode().Perm(); perm&0o077 != 0 {
			t.Fatalf("workdir mode = %o, want no group/other bits", perm)
		}
	}
}
