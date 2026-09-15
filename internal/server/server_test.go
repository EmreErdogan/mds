package server

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func testRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs("../../testdata")
	if err != nil {
		t.Fatal(err)
	}
	return root
}

func get(t *testing.T, h http.Handler, path string) (int, string) {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", path, nil))
	return rec.Code, rec.Body.String()
}

func TestDirectoryMode(t *testing.T) {
	s, err := New(Options{Root: testRoot(t)})
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		path     string
		code     int
		contains string
	}{
		{"/", 200, `href="/README.md"`},
		{"/", 200, `href="/sub/"`},
		{"/README.md", 200, "<h1 id=\"test-document\">Test Document</h1>"},
		{"/README.md", 200, "<title>Test Document</title>"},
		{"/README.md?raw", 200, "# Test Document"},
		{"/sub/page.md", 200, "<title>page.md</title>"},
		{"/sub", 301, ""},
		{"/notes.txt", 200, "plain notes"},
		{"/.hidden.md", 404, ""},
		{"/missing.md", 404, ""},
	}
	for _, c := range cases {
		code, body := get(t, s, c.path)
		if code != c.code {
			t.Errorf("%s: got %d, want %d", c.path, code, c.code)
		}
		if c.contains != "" && !strings.Contains(body, c.contains) {
			t.Errorf("%s: body missing %q", c.path, c.contains)
		}
	}
	if _, body := get(t, s, "/"); strings.Contains(body, ".hidden") {
		t.Error("listing shows hidden file")
	}
}

func TestExtFilter(t *testing.T) {
	s, err := New(Options{Root: testRoot(t), Exts: []string{"md"}})
	if err != nil {
		t.Fatal(err)
	}
	if code, _ := get(t, s, "/notes.txt"); code != 404 {
		t.Errorf("notes.txt: got %d, want 404", code)
	}
	if _, body := get(t, s, "/"); strings.Contains(body, "notes.txt") {
		t.Error("listing shows filtered file")
	}
	if code, _ := get(t, s, "/README.md"); code != 200 {
		t.Errorf("README.md: got %d, want 200", code)
	}
}

func TestFileMode(t *testing.T) {
	root := testRoot(t)
	s, err := New(Options{Root: root, Index: filepath.Join(root, "README.md")})
	if err != nil {
		t.Fatal(err)
	}
	if code, body := get(t, s, "/"); code != 200 || !strings.Contains(body, "<title>Test Document</title>") {
		t.Errorf("/: got %d", code)
	}
	if code, body := get(t, s, "/?raw"); code != 200 || !strings.HasPrefix(body, "# Test Document") {
		t.Errorf("/?raw: got %d, body %q", code, body[:min(20, len(body))])
	}
	if code, _ := get(t, s, "/sub/"); code != 404 {
		t.Errorf("/sub/: got %d, want 404 (no listings in file mode)", code)
	}
	if code, _ := get(t, s, "/notes.txt"); code != 200 {
		t.Errorf("/notes.txt: got %d, want 200 (assets next to the file)", code)
	}
}

func TestLiveReloadEvents(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "doc.md")
	if err := os.WriteFile(file, []byte("# one"), 0o644); err != nil {
		t.Fatal(err)
	}
	s, err := New(Options{Root: dir, Reload: true})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	if _, body := get(t, s, "/doc.md"); !strings.Contains(body, "/_mds/events") {
		t.Error("page missing reload script")
	}

	srv := httptest.NewServer(s)
	defer srv.Close()
	resp, err := http.Get(srv.URL + "/_mds/events?path=/doc.md")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if ct := resp.Header.Get("Content-Type"); ct != "text/event-stream" {
		t.Fatalf("content-type %q", ct)
	}
	// Read the initial comment so the subscription is established.
	buf := make([]byte, 64)
	if _, err := resp.Body.Read(buf); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, []byte("# two"), 0o644); err != nil {
		t.Fatal(err)
	}
	done := make(chan string, 1)
	go func() {
		b, _ := io.ReadAll(resp.Body)
		done <- string(b)
	}()
	select {
	case body := <-done:
		if !strings.Contains(body, "event: reload") {
			t.Errorf("no reload event, got %q", body)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for reload event")
	}
}

func TestNoReload(t *testing.T) {
	s, err := New(Options{Root: testRoot(t)})
	if err != nil {
		t.Fatal(err)
	}
	if _, body := get(t, s, "/README.md"); strings.Contains(body, "/_mds/events") {
		t.Error("reload script present when disabled")
	}
	if code, _ := get(t, s, "/_mds/events?path=/README.md"); code != 404 {
		t.Errorf("events endpoint: got %d, want 404", code)
	}
}
