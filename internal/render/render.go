// Package render converts markdown to HTML.
package render

import (
	"bytes"
	"strings"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/renderer/html"
	"github.com/yuin/goldmark/text"
)

var md = goldmark.New(
	goldmark.WithExtensions(extension.GFM),
	goldmark.WithParserOptions(parser.WithAutoHeadingID()),
	goldmark.WithRendererOptions(html.WithUnsafe()),
)

// Result is a rendered markdown document.
type Result struct {
	HTML  []byte
	Title string // text of the first level-1 heading, or "" if none
}

// Markdown renders GitHub Flavored Markdown to HTML.
func Markdown(src []byte) (Result, error) {
	doc := md.Parser().Parse(text.NewReader(src))
	var buf bytes.Buffer
	if err := md.Renderer().Render(&buf, src, doc); err != nil {
		return Result{}, err
	}
	return Result{HTML: buf.Bytes(), Title: firstH1(doc, src)}, nil
}

func firstH1(doc ast.Node, src []byte) string {
	var title string
	_ = ast.Walk(doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		if h, ok := n.(*ast.Heading); ok && h.Level == 1 {
			title = nodeText(h, src)
			return ast.WalkStop, nil
		}
		return ast.WalkContinue, nil
	})
	return strings.TrimSpace(title)
}

func nodeText(n ast.Node, src []byte) string {
	var b strings.Builder
	_ = ast.Walk(n, func(c ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		switch t := c.(type) {
		case *ast.Text:
			b.Write(t.Segment.Value(src))
		case *ast.String:
			b.Write(t.Value)
		}
		return ast.WalkContinue, nil
	})
	return b.String()
}

// IsMarkdown reports whether the file name has a markdown extension.
func IsMarkdown(name string) bool {
	switch strings.ToLower(ext(name)) {
	case "md", "markdown", "mdown", "mkd", "mkdn":
		return true
	}
	return false
}

func ext(name string) string {
	i := strings.LastIndex(name, ".")
	if i < 0 || i == len(name)-1 {
		return ""
	}
	return name[i+1:]
}
