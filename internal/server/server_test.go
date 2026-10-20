package server

import (
	"archive/zip"
	"bytes"
	"encoding/base64"
	"image"
	"image/png"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/EmreErdogan/mds/internal/config"
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
	s, err := New(Options{Root: root, DirIndex: IndexReadme})
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
	filtered, _ := New(Options{Root: root, DirIndex: IndexReadme, Types: []string{"txt"}})
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
	if _, body := get(t, auto, "/README.md"); !strings.Contains(body, `class="theme" data-server-theme="" data-server-accent=""`) || !strings.Contains(body, `localStorage.getItem("mds-theme")`) {
		t.Error("theme switcher missing")
	}
	dark, _ := New(Options{Root: root, Theme: "dark"})
	if _, body := get(t, dark, "/README.md"); !strings.Contains(body, `<html lang="en" data-theme="dark">`) || !strings.Contains(body, `data-server-theme="dark"`) {
		t.Error("dark theme not applied to html element")
	}
	teal, _ := New(Options{Root: root, Accent: "teal"})
	if _, body := get(t, teal, "/README.md"); !strings.Contains(body, `<html lang="en" data-accent="teal">`) || !strings.Contains(body, `data-server-accent="teal"`) {
		t.Error("accent not applied to html element")
	}
}

// Every accent the config accepts needs a palette, a swatch in the theme menu
// and a place in the early script that restores a reader's choice.
func TestAccentsComplete(t *testing.T) {
	s, _ := New(Options{Root: testRoot(t)})
	_, body := get(t, s, "/README.md")
	for _, a := range config.Accents {
		for _, want := range []string{"[data-accent=" + a + "]{--dir:", `<button type="button" data-accent="` + a + `"`} {
			if !strings.Contains(body, want) {
				t.Errorf("accent %s: page missing %s", a, want)
			}
		}
	}
	if want := "/^(" + strings.Join(config.Accents, "|") + ")$/"; !strings.Contains(body, want) {
		t.Errorf("early script should accept exactly %s", want)
	}
}

func TestPrintStyles(t *testing.T) {
	s, _ := New(Options{Root: testRoot(t)})
	_, body := get(t, s, "/README.md")
	if !strings.Contains(body, "@media print{") || !strings.Contains(body, ".totop,.anchor,.theme,.toc-btn,.dl{display:none!important}") {
		t.Error("print styles missing")
	}
}

func TestStdinContent(t *testing.T) {
	s, err := New(Options{Root: t.TempDir(), Content: []byte("# From Pipe\n\ntext\n"), Reload: true})
	if err != nil {
		t.Fatal(err)
	}
	code, body := get(t, s, "/")
	if code != 200 || !strings.Contains(body, "<title>From Pipe</title>") {
		t.Errorf("/: %d %s", code, body[:min(80, len(body))])
	}
	if strings.Contains(body, "/_mds/events") {
		t.Error("reload script present for in-memory content")
	}
	if code, body := get(t, s, "/?raw"); code != 200 || !strings.HasPrefix(body, "# From Pipe") {
		t.Errorf("/?raw: %d", code)
	}
	for _, p := range []string{"/other.md", "/sub/", "/_mds/events?path=/"} {
		if code, _ := get(t, s, p); code != 404 {
			t.Errorf("%s: got %d, want 404", p, code)
		}
	}
	untitled, _ := New(Options{Root: t.TempDir(), Content: []byte("no heading")})
	if _, body := get(t, untitled, "/"); !strings.Contains(body, "<title>stdin</title>") {
		t.Error("fallback title should be stdin")
	}
}

func TestHTMLIndex(t *testing.T) {
	root := t.TempDir()
	write := func(name, body string) {
		t.Helper()
		p := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("README.md", "# Root Readme\n")
	write("index.html", "<p>root site</p>")
	write("site/index.html", "<p>sub site</p>")
	write("site/app.js", "//js")
	write("plain/notes.txt", "notes")

	const listing, readme = `class="listing"`, `class="dir-index"`
	cases := []struct {
		mode, path string
		want       []string
		not        []string
	}{
		// README wins over index.html and is shown under the listing.
		{IndexReadme, "/", []string{listing, readme, `href="/index.html"`}, []string{"root site"}},
		{IndexReadme, "/site/", []string{"sub site"}, []string{listing}},
		{IndexReadme, "/site/?list", []string{listing, `href="/site/app.js"`}, []string{"sub site"}},
		{IndexReadme, "/plain/", []string{listing}, nil},
		{IndexHTML, "/", []string{"root site"}, []string{listing}},
		{IndexHTML, "/?list", []string{listing, readme}, []string{"root site"}},
		{IndexHTML, "/site/", []string{"sub site"}, nil},
		{IndexList, "/", []string{listing}, []string{readme, "root site"}},
		{IndexList, "/site/", []string{listing}, []string{"sub site"}},
		// index.html stays reachable by name in every mode.
		{IndexReadme, "/index.html", []string{"root site"}, nil},
		{IndexHTML, "/site/index.html", []string{"sub site"}, nil},
		{IndexList, "/site/index.html", []string{"sub site"}, nil},
	}
	for _, c := range cases {
		s, err := New(Options{Root: root, DirIndex: c.mode})
		if err != nil {
			t.Fatal(err)
		}
		code, body := get(t, s, c.path)
		if code != 200 {
			t.Errorf("%s %s: got %d", c.mode, c.path, code)
		}
		for _, w := range c.want {
			if !strings.Contains(body, w) {
				t.Errorf("%s %s: body missing %q", c.mode, c.path, w)
			}
		}
		for _, n := range c.not {
			if strings.Contains(body, n) {
				t.Errorf("%s %s: body has %q", c.mode, c.path, n)
			}
		}
	}

	// The slash redirect keeps the query, and filters apply to index.html.
	s, _ := New(Options{Root: root, DirIndex: IndexHTML})
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, httptest.NewRequest("GET", "/site?list", nil))
	if loc := rec.Header().Get("Location"); rec.Code != 301 || loc != "/site/?list" {
		t.Errorf("redirect: %d %q", rec.Code, loc)
	}
	filtered, _ := New(Options{Root: root, DirIndex: IndexHTML, Types: []string{"md"}})
	if _, body := get(t, filtered, "/site/"); strings.Contains(body, "sub site") {
		t.Error("index.html served although html is filtered out")
	}
}

func TestHTMLReload(t *testing.T) {
	root := t.TempDir()
	for name, body := range map[string]string{
		"index.html":    "<html><body><p>home</p></BODY></html>",
		"bare.html":     "<p>bare</p>",
		"tr.html":       "<html><body><p>İSTANBUL İÇİN</p><script>1</script>\n</body></html>",
		"css/style.css": "p{}",
		"a b.md":        "# spaced",
	} {
		p := filepath.Join(root, name)
		os.MkdirAll(filepath.Dir(p), 0o755)
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	s, err := New(Options{Root: root, Reload: true, DirIndex: IndexReadme})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	_, body := get(t, s, "/")
	if at := strings.Index(body, "/_mds/events"); at < 0 || at > strings.Index(body, "</BODY>") {
		t.Errorf("script not injected before </body>: %q", body)
	}
	if !strings.HasPrefix(body, "<html><body><p>home</p><script>") {
		t.Errorf("page content changed: %q", body)
	}
	if _, body := get(t, s, "/bare.html"); !strings.HasPrefix(body, "<p>bare</p><script>") {
		t.Errorf("script not appended: %q", body)
	}
	// Letters whose lowercase form has another byte length must not shift
	// the insertion point.
	if _, body := get(t, s, "/tr.html"); !strings.HasPrefix(body, "<html><body><p>İSTANBUL İÇİN</p><script>1</script>\n<script>") || !strings.HasSuffix(body, "</script></body></html>") {
		t.Errorf("script not injected right before </body>: %q", body)
	}
	if _, body := get(t, s, "/index.html?raw"); body != "<html><body><p>home</p></BODY></html>" {
		t.Errorf("?raw altered the file: %q", body)
	}
	if _, body := get(t, s, "/css/style.css"); body != "p{}" {
		t.Errorf("non-HTML file altered: %q", body)
	}
	off, _ := New(Options{Root: root, DirIndex: IndexReadme})
	if _, body := get(t, off, "/"); strings.Contains(body, "<script>") {
		t.Error("script injected although reload is off")
	}

	// One stream watches several paths; unknown ones are skipped.
	srv := httptest.NewServer(s)
	defer srv.Close()
	resp, err := http.Get(srv.URL + "/_mds/events?path=/&path=/missing/&path=/css/&path=/a%20b.md")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("status %d", resp.StatusCode)
	}
	buf := make([]byte, 64)
	if _, err := resp.Body.Read(buf); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "css", "style.css"), []byte("p{color:red}"), 0o644); err != nil {
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
	if resp, err := http.Get(srv.URL + "/_mds/events?path=/missing/"); err != nil || resp.StatusCode != 404 {
		t.Errorf("unknown path: %v %v", resp, err)
	}
}

func TestHTMLFileMode(t *testing.T) {
	root := t.TempDir()
	file := filepath.Join(root, "page.html")
	if err := os.WriteFile(file, []byte("<p>just html</p>"), 0o644); err != nil {
		t.Fatal(err)
	}
	s, err := New(Options{Root: root, Index: file})
	if err != nil {
		t.Fatal(err)
	}
	if code, body := get(t, s, "/"); code != 200 || body != "<p>just html</p>" {
		t.Errorf("got %d %q", code, body)
	}
}

func TestSearch(t *testing.T) {
	root := t.TempDir()
	for name, body := range map[string]string{
		"guide.md":            "# Guide\n\nInstall the Widget first.\nwidget again\nWIDGET three\nwidget four\n",
		"docs/widget-api.md":  "# API\n\nnothing here\n",
		"docs/notes.txt":      "a note about <b>widgets</b> & more\n",
		"docs/a b.md":         "çay WIDGET içer\n",
		"bin/blob.dat":        "widget\x00binary",
		".secret/keys.md":     "widget secret",
		"node_modules/pkg.md": "widget dependency",
	} {
		p := filepath.Join(root, name)
		os.MkdirAll(filepath.Dir(p), 0o755)
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	s, err := New(Options{Root: root, Exclude: []string{"node_modules"}})
	if err != nil {
		t.Fatal(err)
	}

	if _, body := get(t, s, "/"); !strings.Contains(body, `action="/_mds/search"`) {
		t.Error("listing has no search box")
	}
	code, body := get(t, s, "/_mds/search?q=Widget")
	if code != 200 {
		t.Fatalf("got %d", code)
	}
	for _, want := range []string{
		"4 results",
		`value="Widget"`,
		`href="/guide.md"`,
		`href="/guide.md#:~:text=Widget"`,
		"Install the <mark>Widget</mark> first.",
		"+1 more matching lines",
		`href="/docs/widget-api.md"`,
		"&lt;b&gt;<mark>widget</mark>s&lt;/b&gt; &amp; more",
		`href="/docs/a%20b.md"`,
		"çay <mark>WIDGET</mark> içer",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("results missing %q", want)
		}
	}
	for _, not := range []string{"blob.dat", "keys.md", "pkg.md", "/_mds/events"} {
		if strings.Contains(body, not) {
			t.Errorf("results contain %q", not)
		}
	}
	// The file matched by name comes before content-only matches.
	if strings.Index(body, "widget-api.md") > strings.Index(body, `href="/guide.md"`) {
		t.Error("name match not listed first")
	}
	// A slash matches against the path; names alone do not match directories' files.
	if _, body := get(t, s, "/_mds/search?q=docs/wid"); !strings.Contains(body, "1 result ") || !strings.Contains(body, "widget-api.md") {
		t.Errorf("path query: %q", body)
	}
	if _, body := get(t, s, "/_mds/search?q=docs"); !strings.Contains(body, `href="/docs/"`) || strings.Contains(body, "notes.txt") {
		t.Error("directory name query should list only the directory")
	}
	if _, body := get(t, s, "/_mds/search?q=zzzz"); !strings.Contains(body, "No results") {
		t.Error("missing empty state")
	}
	if code, body := get(t, s, "/_mds/search"); code != 200 || !strings.Contains(body, "Type in the search box") {
		t.Errorf("empty query: %d", code)
	}

	typed, _ := New(Options{Root: root, Types: []string{"txt"}})
	if _, body := get(t, typed, "/_mds/search?q=widget"); !strings.Contains(body, "notes.txt") || strings.Contains(body, "guide.md") {
		t.Error("types filter not applied to search")
	}
	single, _ := New(Options{Root: root, Index: filepath.Join(root, "guide.md")})
	if code, _ := get(t, single, "/_mds/search?q=widget"); code != 404 {
		t.Errorf("search in single-file mode: %d", code)
	}
	if _, body := get(t, single, "/"); strings.Contains(body, `action="/_mds/search"`) {
		t.Error("search box shown in single-file mode")
	}
}

func TestDownload(t *testing.T) {
	root := t.TempDir()
	var pngData bytes.Buffer
	if err := png.Encode(&pngData, image.NewRGBA(image.Rect(0, 0, 4, 4))); err != nil {
		t.Fatal(err)
	}
	files := map[string][]byte{
		"docs/Çalışma notu.md": []byte("# Notes\n\n![pic](../img/pic.png) ![secret](../.private/s.png) ![far](https://example.com/x.png)\n"),
		"img/pic.png":          pngData.Bytes(),
		".private/s.png":       pngData.Bytes(),
	}
	for name, body := range files {
		p := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, body, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	s, err := New(Options{Root: root})
	if err != nil {
		t.Fatal(err)
	}
	fetch := func(url string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		s.ServeHTTP(rec, httptest.NewRequest("GET", url, nil))
		return rec
	}
	const page = "/docs/%C3%87al%C4%B1%C5%9Fma%20notu.md"

	_, body := get(t, s, page)
	for _, want := range []string{`class="dl"`, `?download=md`, `?download=html`, `?download=docx`, `class="dl-pdf"`} {
		if !strings.Contains(body, want) {
			t.Errorf("page missing %q", want)
		}
	}
	if _, body := get(t, s, "/docs/"); strings.Contains(body, `class="dl"`) {
		t.Error("listing should not offer downloads")
	}

	rec := fetch(page + "?download=md")
	if rec.Code != 200 || rec.Body.String() != string(files["docs/Çalışma notu.md"]) {
		t.Errorf("md: %d %q", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Content-Disposition"); got != "attachment; filename*=utf-8''%C3%87al%C4%B1%C5%9Fma%20notu.md" {
		t.Errorf("md disposition: %q", got)
	}

	rec = fetch(page + "?download=html")
	html := rec.Body.String()
	if rec.Code != 200 || !strings.HasPrefix(rec.Header().Get("Content-Type"), "text/html") || !strings.Contains(rec.Header().Get("Content-Disposition"), "notu.html") {
		t.Errorf("html: %d %v", rec.Code, rec.Header())
	}
	if !strings.Contains(html, `<title>Notes</title>`) || !strings.Contains(html, `src="data:image/png;base64,`) {
		t.Error("html export missing title or inlined image")
	}
	if !strings.Contains(html, `src="../.private/s.png"`) || !strings.Contains(html, `src="https://example.com/x.png"`) {
		t.Error("hidden or remote images should not be inlined")
	}
	for _, unwanted := range []string{"<nav>", "/_mds/", `class="theme"`} {
		if strings.Contains(html, unwanted) {
			t.Errorf("html export contains %q", unwanted)
		}
	}

	rec = fetch(page + "?download=docx")
	if rec.Code != 200 || rec.Header().Get("Content-Type") != "application/vnd.openxmlformats-officedocument.wordprocessingml.document" {
		t.Fatalf("docx: %d %v", rec.Code, rec.Header())
	}
	data := rec.Body.Bytes()
	z, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	var media []string
	for _, f := range z.File {
		if strings.HasPrefix(f.Name, "word/media/") {
			media = append(media, f.Name)
		}
	}
	if len(media) != 1 {
		t.Errorf("docx media = %v, want only the visible local image", media)
	}

	if rec := fetch(page + "?download=exe"); rec.Code != 400 {
		t.Errorf("unknown format: got %d, want 400", rec.Code)
	}
}

func TestDownloadSingleAndStdin(t *testing.T) {
	root := t.TempDir()
	file := filepath.Join(root, "one.md")
	if err := os.WriteFile(file, []byte("# One\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	single, _ := New(Options{Root: root, Index: file})
	if _, body := get(t, single, "/"); !strings.Contains(body, `href="/?download=docx"`) {
		t.Error("single-file page should download from /")
	}
	rec := httptest.NewRecorder()
	single.ServeHTTP(rec, httptest.NewRequest("GET", "/?download=md", nil))
	if rec.Body.String() != "# One\n" || rec.Header().Get("Content-Disposition") != `attachment; filename=one.md` {
		t.Errorf("single md: %q %v", rec.Body.String(), rec.Header())
	}

	stdin, _ := New(Options{Root: root, Content: []byte("# Piped\n")})
	rec = httptest.NewRecorder()
	stdin.ServeHTTP(rec, httptest.NewRequest("GET", "/?download=docx", nil))
	if rec.Code != 200 || rec.Header().Get("Content-Disposition") != `attachment; filename=stdin.docx` {
		t.Errorf("stdin docx: %d %v", rec.Code, rec.Header())
	}
}

func TestDownloadDiagrams(t *testing.T) {
	root := t.TempDir()
	src := "# D\n\n```mermaid\ngraph LR; A-->B & C\n```\n"
	if err := os.WriteFile(filepath.Join(root, "d.md"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "n.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	s, _ := New(Options{Root: root})
	var pngData bytes.Buffer
	if err := png.Encode(&pngData, image.NewRGBA(image.Rect(0, 0, 20, 10))); err != nil {
		t.Fatal(err)
	}
	diagrams := `[{"src":"graph LR; A-->B & C\n","svg":"<svg xmlns='http://www.w3.org/2000/svg'/>","png":"` +
		base64.StdEncoding.EncodeToString(pngData.Bytes()) + `","width":10.4,"height":5}]`
	post := func(url string) *httptest.ResponseRecorder {
		var body bytes.Buffer
		mw := multipart.NewWriter(&body)
		_ = mw.WriteField("diagrams", diagrams)
		mw.Close()
		req := httptest.NewRequest("POST", url, &body)
		req.Header.Set("Content-Type", mw.FormDataContentType())
		rec := httptest.NewRecorder()
		s.ServeHTTP(rec, req)
		return rec
	}

	rec := post("/d.md?download=html")
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `<p class="diagram"><img src="data:image/svg+xml;base64,`) ||
		!strings.Contains(rec.Body.String(), `width="10" height="5"`) {
		t.Errorf("html: %d", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "mermaid.min.js") {
		t.Error("export with all diagrams drawn should not load mermaid")
	}
	if _, body := get(t, s, "/d.md?download=html"); !strings.Contains(body, `class="language-mermaid"`) {
		t.Error("a plain GET should keep the diagram as code")
	}

	rec = post("/d.md?download=docx")
	data := rec.Body.Bytes()
	z, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, f := range z.File {
		names = append(names, f.Name)
	}
	if !strings.Contains(strings.Join(names, " "), "word/media/image1.svg") {
		t.Errorf("docx parts: %v", names)
	}

	for _, url := range []string{"/d.md", "/d.md?download=docx&raw", "/n.txt?download=md", "/?download=md", "/_mds/search?download=md"} {
		if rec := post(url); rec.Code != 405 {
			t.Errorf("POST %s: got %d, want 405", url, rec.Code)
		}
	}
}

func TestListingModTime(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "a.md")
	if err := os.WriteFile(f, []byte("# a"), 0o644); err != nil {
		t.Fatal(err)
	}
	mt := time.Date(2020, 3, 4, 5, 6, 7, 0, time.UTC)
	if err := os.Chtimes(f, mt, mt); err != nil {
		t.Fatal(err)
	}
	s, err := New(Options{Root: dir})
	if err != nil {
		t.Fatal(err)
	}
	_, body := get(t, s, "/")
	// html/template writes "+" in attributes as "&#43;".
	want := strings.ReplaceAll(`datetime="`+mt.Local().Format(time.RFC3339)+`" title="`+fullTime(mt.Local())+`"`, "+", "&#43;")
	if !strings.Contains(body, want) {
		t.Errorf("listing missing %s", want)
	}
}

func TestRelativeTime(t *testing.T) {
	now := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		ago  time.Duration
		want string
	}{
		{10 * time.Second, "now"},
		{time.Minute, "1 minute ago"},
		{5 * time.Minute, "5 minutes ago"},
		{3 * time.Hour, "3 hours ago"},
		{30 * time.Hour, "yesterday"},
		{4 * 24 * time.Hour, "4 days ago"},
		{9 * 24 * time.Hour, "last week"},
		{22 * 24 * time.Hour, "3 weeks ago"},
		{40 * 24 * time.Hour, "last month"},
		{100 * 24 * time.Hour, "3 months ago"},
		{400 * 24 * time.Hour, "last year"},
		{1000 * 24 * time.Hour, "2 years ago"},
		{-time.Hour, "now"},
	}
	for _, c := range cases {
		if got := relativeTime(now.Add(-c.ago), now); got != c.want {
			t.Errorf("%v ago: got %q, want %q", c.ago, got, c.want)
		}
	}
}

func TestFullTime(t *testing.T) {
	cases := []struct {
		offset int
		want   string
	}{
		{0, "Oct 20, 2026, 14:03:05 GMT"},
		{3 * 3600, "Oct 20, 2026, 14:03:05 GMT+3"},
		{-(4*3600 + 30*60), "Oct 20, 2026, 14:03:05 GMT-4:30"},
	}
	for _, c := range cases {
		tm := time.Date(2026, 10, 20, 14, 3, 5, 0, time.FixedZone("", c.offset))
		if got := fullTime(tm); got != c.want {
			t.Errorf("offset %d: got %q, want %q", c.offset, got, c.want)
		}
	}
}

func TestListingFolderModTime(t *testing.T) {
	dir := t.TempDir()
	old := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	newer := time.Date(2024, 6, 1, 0, 0, 0, 0, time.UTC)
	newest := time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC)
	write := func(rel string, mt time.Time) {
		p := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(p, mt, mt); err != nil {
			t.Fatal(err)
		}
	}
	write("docs/deep/a.md", newer)
	// Hidden and filtered files must not count.
	write("docs/.secret.md", newest)
	write("docs/deep/b.bin", newest)
	for _, d := range []string{"docs/deep", "docs"} {
		if err := os.Chtimes(filepath.Join(dir, d), old, old); err != nil {
			t.Fatal(err)
		}
	}
	s, err := New(Options{Root: dir, Types: []string{"md"}})
	if err != nil {
		t.Fatal(err)
	}
	_, body := get(t, s, "/")
	want := strings.ReplaceAll(`title="`+fullTime(newer.Local())+`"`, "+", "&#43;")
	if !strings.Contains(body, want) {
		t.Errorf("folder should carry its newest visible file's time %s", want)
	}
}

func TestListingIcons(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "a.md"), []byte("# a"), 0o644); err != nil {
		t.Fatal(err)
	}
	s, err := New(Options{Root: dir})
	if err != nil {
		t.Fatal(err)
	}
	_, body := get(t, s, "/")
	for _, want := range []string{
		`<a href="/sub/"><svg class="icon dir"`,
		`<a href="/a.md"><svg class="icon"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("listing missing %s", want)
		}
	}
}

func TestListingReadmeTOC(t *testing.T) {
	dir := t.TempDir()
	readme := "# Project\n\n## Install\n\n## Usage\n\n## License\n"
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte(readme), 0o644); err != nil {
		t.Fatal(err)
	}
	on, _ := New(Options{Root: dir, DirIndex: IndexReadme, TOC: true})
	if _, body := get(t, on, "/"); !strings.Contains(body, `class="has-toc"`) || !strings.Contains(body, `href="#usage"`) {
		t.Error("listing with a README should show its table of contents")
	}
	off, _ := New(Options{Root: dir, DirIndex: IndexReadme, TOC: false})
	if _, body := get(t, off, "/"); strings.Contains(body, `class="has-toc"`) {
		t.Error("toc shown while disabled")
	}
	list, _ := New(Options{Root: dir, DirIndex: IndexList, TOC: true})
	if _, body := get(t, list, "/"); strings.Contains(body, `class="has-toc"`) {
		t.Error("toc shown for a listing without a rendered README")
	}
}
