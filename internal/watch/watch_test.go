package watch

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func wait(t *testing.T, ch <-chan struct{}, what string) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(2 * time.Second):
		t.Fatalf("no notification for %s", what)
	}
}

func TestFileAndDirNotifications(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "a.md")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	h, err := New()
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()

	fileCh, cancelFile := h.Subscribe(file)
	defer cancelFile()
	dirCh, cancelDir := h.Subscribe(dir)
	defer cancelDir()

	if err := os.WriteFile(file, []byte("y"), 0o644); err != nil {
		t.Fatal(err)
	}
	wait(t, fileCh, "file write")
	wait(t, dirCh, "dir on file write")

	// Atomic save: write temp, rename over.
	tmp := filepath.Join(dir, "a.md.tmp")
	if err := os.WriteFile(tmp, []byte("z"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(tmp, file); err != nil {
		t.Fatal(err)
	}
	wait(t, fileCh, "atomic save")

	// A new sibling notifies the dir subscriber only.
	drain(fileCh)
	drain(dirCh)
	if err := os.WriteFile(filepath.Join(dir, "b.md"), []byte("b"), 0o644); err != nil {
		t.Fatal(err)
	}
	wait(t, dirCh, "new sibling")
	select {
	case <-fileCh:
		t.Error("file subscriber notified for sibling change")
	case <-time.After(200 * time.Millisecond):
	}
}

func drain(ch <-chan struct{}) {
	for {
		select {
		case <-ch:
		default:
			return
		}
	}
}
