package server

import (
	"bytes"
	"errors"
	"fmt"
	"html/template"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode/utf8"
)

// searchPath is the reserved URL for the search results page.
const searchPath = "/_mds/search"

const (
	// searchMaxHits caps the number of files on a results page.
	searchMaxHits = 100
	// searchMaxLines caps the matching lines shown per file.
	searchMaxLines = 3
	// searchMaxFileSize is the largest file whose contents are searched.
	searchMaxFileSize = 1 << 20
	// searchTimeout bounds one search so a huge tree cannot stall the server.
	searchTimeout = 3 * time.Second
)

type searchHit struct {
	Path      string // slash-separated, relative to the root
	URL       string
	NameMatch bool
	Lines     []searchLine
	More      int // matching lines beyond those shown
}

type searchLine struct {
	Num                  int
	URL                  string
	Before, Match, After string
}

var errSearchStop = errors.New("search stopped")

// search walks the served tree for files whose name or text content contains
// query, ignoring case. A query with a slash is matched against the relative
// path instead of the name. truncated reports that the walk ended early.
func (s *Server) search(query string, deadline time.Time) (hits []searchHit, truncated bool) {
	q := strings.ToLower(query)
	byPath := strings.Contains(q, "/")
	_ = filepath.WalkDir(s.root, func(p string, d fs.DirEntry, err error) error {
		if err != nil || p == s.root {
			return nil
		}
		if s.blockedName(d.Name()) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if !d.IsDir() && !s.allowedType(d.Name()) {
			return nil
		}
		if len(hits) == searchMaxHits || time.Now().After(deadline) {
			truncated = true
			return errSearchStop
		}
		rel, err := filepath.Rel(s.root, p)
		if err != nil {
			return nil
		}
		rel = filepath.ToSlash(rel)
		hit := searchHit{Path: rel, URL: (&url.URL{Path: "/" + rel}).EscapedPath()}
		name := d.Name()
		if byPath {
			name = rel
		}
		hit.NameMatch = strings.Contains(strings.ToLower(name), q)
		if d.IsDir() {
			hit.Path += "/"
			hit.URL += "/"
		} else {
			hit.Lines, hit.More = searchFile(p, d, query, hit.URL)
		}
		if hit.NameMatch || len(hit.Lines) > 0 {
			hits = append(hits, hit)
		}
		return nil
	})
	// Name matches first; the walk already yields lexical order.
	sort.SliceStable(hits, func(i, j int) bool { return hits[i].NameMatch && !hits[j].NameMatch })
	return hits, truncated
}

// searchFile returns the first lines of a text file that contain query, and
// how many further lines match.
func searchFile(fsPath string, d fs.DirEntry, query, fileURL string) (lines []searchLine, more int) {
	info, err := d.Info()
	if err != nil || !info.Mode().IsRegular() || info.Size() > searchMaxFileSize {
		return nil, 0
	}
	data, err := os.ReadFile(fsPath)
	if err != nil || bytes.IndexByte(data[:min(len(data), 8000)], 0) >= 0 {
		return nil, 0
	}
	q := strings.ToLower(query)
	if !bytes.Contains(bytes.ToLower(data), []byte(q)) {
		return nil, 0
	}
	// A text fragment makes the browser scroll to and highlight the match.
	lineURL := fileURL + "#:~:text=" + strings.NewReplacer("-", "%2D", ",", "%2C", "&", "%26").Replace(url.PathEscape(query))
	for i, line := range strings.Split(string(data), "\n") {
		lower := strings.ToLower(line)
		at := strings.Index(lower, q)
		if at < 0 {
			continue
		}
		if len(lines) == searchMaxLines {
			more++
			continue
		}
		l := searchLine{Num: i + 1, URL: lineURL}
		if end := at + len(q); len(lower) == len(line) && utf8.ValidString(line[:at]) && utf8.ValidString(line[at:end]) {
			l.Before, l.Match, l.After = clip(strings.TrimLeft(line[:at], " \t"), 60, true), line[at:end], clip(strings.TrimRight(line[end:], " \t\r"), 120, false)
		} else {
			// Lowercasing shifted byte offsets; show the line unhighlighted.
			l.After = clip(strings.TrimSpace(line), 180, false)
		}
		lines = append(lines, l)
	}
	return lines, more
}

// clip shortens s to at most n runes, keeping its end when tail is set.
func clip(s string, n int, tail bool) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	if tail {
		return "…" + string(r[len(r)-n:])
	}
	return string(r[:n]) + "…"
}

func (s *Server) serveSearch(w http.ResponseWriter, r *http.Request) {
	query := strings.TrimSpace(r.URL.Query().Get("q"))
	data := map[string]any{"Query": query}
	if query != "" {
		hits, truncated := s.search(query, time.Now().Add(searchTimeout))
		summary := fmt.Sprintf("%d results", len(hits))
		switch {
		case truncated:
			summary = fmt.Sprintf("First %d results; the search stopped early", len(hits))
		case len(hits) == 0:
			summary = "No results"
		case len(hits) == 1:
			summary = "1 result"
		}
		data["Hits"], data["Summary"] = hits, summary
	}
	var buf bytes.Buffer
	if err := tmpl.ExecuteTemplate(&buf, "search.html", data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	s.render(w, page{
		Title:    "Search",
		Crumbs:   []crumb{{Name: "~", URL: "/"}, {Name: "search"}},
		Content:  template.HTML(buf.String()),
		Query:    query,
		noReload: true,
	})
}
