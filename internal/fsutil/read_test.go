//go:build unix

package fsutil

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestReadBounded(t *testing.T) {
	dir := t.TempDir()
	small := filepath.Join(dir, "small")
	write(t, small, "12345")
	if b, err := ReadBounded(small, 5); err != nil || string(b) != "12345" {
		t.Errorf("at the limit: %q %v", b, err)
	}
	if _, err := ReadBounded(small, 4); err == nil {
		t.Error("over the limit: no error")
	}
	if _, err := ReadBounded(dir, 10); err == nil {
		t.Error("directory: no error")
	}
	// A FIFO is refused without blocking, for reading and for writing.
	fifo := filepath.Join(dir, "fifo")
	if err := syscall.Mkfifo(fifo, 0o644); err != nil {
		t.Skip(err)
	}
	if _, err := ReadBounded(fifo, 10); err == nil {
		t.Error("FIFO: no error")
	}
	if f, err := OpenRegular(fifo, os.O_WRONLY|os.O_APPEND, 0); err == nil {
		f.Close()
		t.Error("FIFO opened for writing")
	}
}

func TestReaderTotalLimit(t *testing.T) {
	dir := t.TempDir()
	for name, n := range map[string]int{"a": 6, "b": 6, "c": 3, "big": 20} {
		write(t, filepath.Join(dir, name), strings.Repeat("x", n))
	}
	r := NewReader(10, 15)
	read := func(name string) error {
		_, err := r.Read(filepath.Join(dir, name))
		return err
	}
	// Over the per-file limit: refused, but the 11 bytes read count.
	if err := read("big"); err == nil || errors.Is(err, ErrTotalLimit) {
		t.Errorf("big: %v", err)
	}
	// 4 bytes left: a 6-byte file is over the total.
	if err := read("a"); !errors.Is(err, ErrTotalLimit) {
		t.Errorf("a: %v", err)
	}
	if err := read("c"); !errors.Is(err, ErrTotalLimit) {
		t.Errorf("c after the total is used up: %v", err)
	}
	// A path is read once: the same result again, without counting.
	r = NewReader(10, 12)
	for range 3 {
		if err := read("a"); err != nil {
			t.Fatalf("a: %v", err)
		}
	}
	if err := read("b"); err != nil {
		t.Errorf("b: %v", err)
	}
}

// A counted file still reads with its content, and is charged once, in
// either order; a symlink to it is the same file.
func TestReaderCount(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "a"), "CONTENT")
	write(t, filepath.Join(dir, "b"), "BBB")
	if err := os.Symlink(filepath.Join(dir, "a"), filepath.Join(dir, "link")); err != nil {
		t.Fatal(err)
	}
	a, b := filepath.Join(dir, "a"), filepath.Join(dir, "b")
	r := NewReader(10, 10)
	if err := r.Count(a, 7); err != nil {
		t.Fatal(err)
	}
	if got, err := r.Read(filepath.Join(dir, "link")); string(got) != "CONTENT" || err != nil {
		t.Errorf("read after count: %q %v", got, err)
	}
	if got, err := r.Read(b); string(got) != "BBB" || err != nil {
		t.Errorf("a was charged twice: %q %v", got, err)
	}
	r = NewReader(10, 10)
	if got, err := r.Read(a); string(got) != "CONTENT" || err != nil {
		t.Fatalf("read: %q %v", got, err)
	}
	if err := r.Count(a, 7); err != nil {
		t.Errorf("count after read: %v", err)
	}
	if got, err := r.Read(a); string(got) != "CONTENT" || err != nil {
		t.Errorf("read after count: %q %v", got, err)
	}
	if _, err := r.Read(b); err != nil {
		t.Errorf("b: %v", err)
	}
	if err := r.Count(filepath.Join(dir, "c"), 1); !errors.Is(err, ErrTotalLimit) {
		t.Errorf("over the total: %v", err)
	}
}
