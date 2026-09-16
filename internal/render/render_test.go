package render

import (
	"strings"
	"testing"
)

func TestFrontMatter(t *testing.T) {
	src := []byte("---\ntitle: From Meta\ntags: [a, b]\n---\n\n# Heading\n\ntext\n")
	res, err := Markdown(src)
	if err != nil {
		t.Fatal(err)
	}
	if res.Title != "From Meta" {
		t.Errorf("title = %q", res.Title)
	}
	html := string(res.HTML)
	if strings.Contains(html, "title:") || strings.Contains(html, "<hr") {
		t.Errorf("front matter leaked into output: %s", html)
	}
	if !strings.Contains(html, "<h1 id=\"heading\">Heading</h1>") {
		t.Errorf("body missing: %s", html)
	}
	if res.Meta["tags"] == nil {
		t.Error("meta not parsed")
	}
}

func TestTitleFallsBackToH1(t *testing.T) {
	res, err := Markdown([]byte("---\ndraft: true\n---\n# Real Title\n"))
	if err != nil {
		t.Fatal(err)
	}
	if res.Title != "Real Title" {
		t.Errorf("title = %q", res.Title)
	}
	res, _ = Markdown([]byte("no headings"))
	if res.Title != "" || res.Meta != nil {
		t.Errorf("plain doc: title %q meta %v", res.Title, res.Meta)
	}
}

func TestInvalidFrontMatterDoesNotBreakRendering(t *testing.T) {
	res, err := Markdown([]byte("---\n: bad: [yaml\n---\n# Still Here\n"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(res.HTML), "Still Here") {
		t.Errorf("body lost: %s", res.HTML)
	}
}
