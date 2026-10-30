package logrotate

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func read(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("read %s: %v", p, err)
	}
	return string(b)
}

func TestRotatesAndKeepsBackups(t *testing.T) {
	p := filepath.Join(t.TempDir(), "proxy.log")
	w, err := New(p, 20, 2)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()

	for _, line := range []string{"aaaaaaaaaa\n", "bbbbbbbbbb\n", "cccccccccc\n", "dddddddddd\n", "eeeeeeeeee\n"} {
		if _, err := w.Write([]byte(line)); err != nil {
			t.Fatal(err)
		}
	}
	// 11-byte lines, limit 20: two lines never fit together.
	if got := read(t, p); got != "eeeeeeeeee\n" {
		t.Fatalf("current file = %q", got)
	}
	if got := read(t, p+".1"); got != "dddddddddd\n" {
		t.Fatalf(".1 = %q", got)
	}
	if got := read(t, p+".2"); got != "cccccccccc\n" {
		t.Fatalf(".2 = %q", got)
	}
	if _, err := os.Stat(p + ".3"); err == nil {
		t.Fatal("more backups than requested were kept")
	}
}

func TestAtLeastOneBackupAndAppendOnReopen(t *testing.T) {
	p := filepath.Join(t.TempDir(), "x.log")
	w, _ := New(p, 15, 0) // 0 -> coerced to 1
	w.Write([]byte("0123456789\n"))
	w.Close()

	w, err := New(p, 15, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	w.Write([]byte("abcdefghij\n")) // does not fit next to the existing 11 bytes
	if got := read(t, p+".1"); got != "0123456789\n" {
		t.Fatalf("backup = %q", got)
	}
	if got := read(t, p); got != "abcdefghij\n" {
		t.Fatalf("current = %q", got)
	}
}

func TestOversizedSingleLineStillWritten(t *testing.T) {
	p := filepath.Join(t.TempDir(), "big.log")
	w, _ := New(p, 5, 1)
	defer w.Close()
	line := strings.Repeat("z", 40) + "\n"
	if n, err := w.Write([]byte(line)); err != nil || n != len(line) {
		t.Fatalf("write: n=%d err=%v", n, err)
	}
	if read(t, p) != line {
		t.Fatal("line lost")
	}
}

func TestUnlimitedWhenMaxBytesZero(t *testing.T) {
	p := filepath.Join(t.TempDir(), "u.log")
	w, _ := New(p, 0, 1)
	defer w.Close()
	for i := 0; i < 100; i++ {
		w.Write([]byte("0123456789\n"))
	}
	if _, err := os.Stat(p + ".1"); err == nil {
		t.Fatal("rotation happened with maxBytes=0")
	}
}
