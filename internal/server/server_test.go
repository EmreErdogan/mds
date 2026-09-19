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
	s, err := New(Options{Root: testRoot(t), Types: []string{"md"}})
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

func TestDirIndex(t *testing.T) {
	root := testRoot(t)
	s, err := New(Options{Root: root, DirIndex: true})
	if err != nil {
		t.Fatal(err)
	}
	_, body := get(t, s, "/")
	if !strings.Contains(body, `class="dir-index"`) || !strings.Contains(body, "<h1 id=\"test-document\">") {
		t.Error("README not rendered under listing")
	}
	if _, body := get(t, s, "/sub/"); strings.Contains(body, `class="dir-index"`) {
		t.Error("dir without README rendered an index")
	}
	off, _ := New(Options{Root: root})
	if _, body := get(t, off, "/"); strings.Contains(body, `class="dir-index"`) {
		t.Error("index rendered while disabled")
	}
	filtered, _ := New(Options{Root: root, DirIndex: true, Types: []string{"txt"}})
	if _, body := get(t, filtered, "/"); strings.Contains(body, `class="dir-index"`) {
		t.Error("index rendered although md is filtered out")
	}
}

func TestExcludeAndHidden(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "node_modules", "pkg"), 0o755)
	os.WriteFile(filepath.Join(dir, "node_modules", "pkg", "x.md"), []byte("x"), 0o644)
	os.WriteFile(filepath.Join(dir, "debug.log"), []byte("log"), 0o644)
	os.WriteFile(filepath.Join(dir, ".secret.md"), []byte("s"), 0o644)
	os.WriteFile(filepath.Join(dir, "ok.md"), []byte("ok"), 0o644)

	s, err := New(Options{Root: dir, Exclude: []string{"node_modules", "*.log"}})
	if err != nil {
		t.Fatal(err)
	}
	_, body := get(t, s, "/")
	for _, bad := range []string{"node_modules", "debug.log", ".secret"} {
		if strings.Contains(body, bad) {
			t.Errorf("listing shows excluded %s", bad)
		}
	}
	if !strings.Contains(body, "ok.md") {
		t.Error("listing missing ok.md")
	}
	for _, p := range []string{"/node_modules/", "/node_modules/pkg/x.md", "/debug.log", "/.secret.md"} {
		if code, _ := get(t, s, p); code != 404 {
			t.Errorf("%s: got %d, want 404", p, code)
		}
	}

	h, _ := New(Options{Root: dir, Hidden: true, Exclude: []string{".git"}})
	if code, _ := get(t, h, "/.secret.md"); code != 200 {
		t.Errorf("hidden file with Hidden=true: got %d", code)
	}
	if _, body := get(t, h, "/"); !strings.Contains(body, ".secret.md") {
		t.Error("hidden file not listed with Hidden=true")
	}
	os.MkdirAll(filepath.Join(dir, ".git"), 0o755)
	if code, _ := get(t, h, "/.git/"); code != 404 {
		t.Errorf(".git should stay excluded even with Hidden=true, got %d", code)
	}

	if _, err := New(Options{Root: dir, Exclude: []string{"[bad"}}); err == nil {
		t.Error("expected error for malformed pattern")
	}
}

func TestMermaidScriptOnlyWhenNeeded(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "diagram.md"), []byte("# D\n\n```mermaid\ngraph TD; A-->B;\n```\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "plain.md"), []byte("# P\n\n```go\nx := 1\n```\n"), 0o644)
	s, _ := New(Options{Root: dir})
	_, body := get(t, s, "/diagram.md")
	if !strings.Contains(body, "mermaid.min.js") || !strings.Contains(body, `class="language-mermaid"`) {
		t.Error("mermaid page missing script or block")
	}
	if _, body := get(t, s, "/plain.md"); strings.Contains(body, "mermaid.min.js") {
		t.Error("mermaid script loaded on a page without diagrams")
	}
}

func TestTOC(t *testing.T) {
	dir := t.TempDir()
	long := "# Title\n\n## One\n\ntext\n\n### One A\n\n## Two\n\n#### Deep\n\n##### Too deep\n"
	os.WriteFile(filepath.Join(dir, "long.md"), []byte(long), 0o644)
	os.WriteFile(filepath.Join(dir, "short.md"), []byte("# T\n\n## Only\n\n## Two\n"), 0o644)

	s, _ := New(Options{Root: dir, TOC: true})
	_, body := get(t, s, "/long.md")
	for _, want := range []string{`class="has-toc"`, `href="#one"`, `href="#one-a"`, `class="l4"`, `class="toc-btn"`, `class="filter"`, `id="toc-menu"`, `class="toc toc-side"`} {
		if !strings.Contains(body, want) {
			t.Errorf("toc missing %q", want)
		}
	}
	if strings.Contains(body, `href="#too-deep"`) || strings.Contains(body, `href="#title"`) {
		t.Error("toc includes h1 or h5")
	}
	if _, body := get(t, s, "/short.md"); strings.Contains(body, `class="has-toc"`) || strings.Contains(body, `class="toc-btn"`) {
		t.Error("toc shown for a document with too few headings")
	}
	if _, body := get(t, s, "/short.md"); !strings.Contains(body, `class="totop"`) {
		t.Error("back-to-top button missing")
	}
	off, _ := New(Options{Root: dir, TOC: false})
	if _, body := get(t, off, "/long.md"); strings.Contains(body, `class="has-toc"`) {
		t.Error("toc shown while disabled")
	}
}

func TestTheme(t *testing.T) {
	root := testRoot(t)
	auto, _ := New(Options{Root: root})
	if _, body := get(t, auto, "/README.md"); !strings.Contains(body, `<html lang="en">`) {
		t.Error("auto theme should not set data-theme")
	}
	if _, body := get(t, auto, "/README.md"); !strings.Contains(body, `class="theme-btn" data-server-theme=""`) || !strings.Contains(body, `localStorage.getItem("mds-theme")`) {
		t.Error("theme switcher missing")
	}
	dark, _ := New(Options{Root: root, Theme: "dark"})
	if _, body := get(t, dark, "/README.md"); !strings.Contains(body, `<html lang="en" data-theme="dark">`) || !strings.Contains(body, `data-server-theme="dark"`) {
		t.Error("dark theme not applied to html element")
	}
}

func TestPrintStyles(t *testing.T) {
	s, _ := New(Options{Root: testRoot(t)})
	_, body := get(t, s, "/README.md")
	if !strings.Contains(body, "@media print{") || !strings.Contains(body, ".totop,.anchor,.theme-btn,.toc-btn{display:none!important}") {
		t.Error("print styles missing")
	}
}
