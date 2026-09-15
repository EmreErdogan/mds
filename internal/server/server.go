// Package server implements the HTTP handler that serves markdown files and
// directory listings.
package server

import (
	"bytes"
	"embed"
	"fmt"
	"html/template"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/EmreErdogan/mds/internal/render"
	"github.com/EmreErdogan/mds/internal/watch"
)

// eventsPath is the reserved URL for the live-reload event stream.
const eventsPath = "/_mds/events"

//go:embed templates/*
var templateFS embed.FS

var tmpl = template.Must(template.ParseFS(templateFS, "templates/*.html"))

// Options configures a Server.
type Options struct {
	// Root is the directory to serve.
	Root string
	// Index, if set, is the absolute path of a single markdown file that is
	// rendered at "/". Directory listings are disabled in this mode.
	Index string
	// Exts restricts served files to these extensions (lowercase, no dot).
	// Empty means all files are served.
	Exts []string
	// Reload enables live reload: pages subscribe to changes of the file or
	// directory they show and reload themselves.
	Reload bool
}

// Server is an http.Handler serving a directory or a single markdown file.
type Server struct {
	root  string
	index string
	exts  map[string]bool
	hub   *watch.Hub // nil when live reload is off
}

// New creates a Server from opts.
func New(opts Options) (*Server, error) {
	root, err := filepath.Abs(opts.Root)
	if err != nil {
		return nil, err
	}
	s := &Server{root: root, index: opts.Index}
	if opts.Reload {
		hub, err := watch.New()
		if err != nil {
			return nil, err
		}
		s.hub = hub
	}
	if len(opts.Exts) > 0 {
		s.exts = make(map[string]bool, len(opts.Exts))
		for _, e := range opts.Exts {
			s.exts[strings.ToLower(e)] = true
		}
	}
	return s, nil
}

type crumb struct {
	Name string
	URL  string
}

type entry struct {
	Name  string
	URL   string
	IsDir bool
	Size  string
}

type page struct {
	Title        string
	Crumbs       []crumb
	Content      template.HTML
	RawURL       string
	Reload       bool
	HighlightCSS template.CSS
}

// Close releases resources held by the Server.
func (s *Server) Close() error {
	if s.hub != nil {
		return s.hub.Close()
	}
	return nil
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	urlPath := path.Clean("/" + r.URL.Path)
	if hasHiddenSegment(urlPath) {
		http.NotFound(w, r)
		return
	}

	if urlPath == eventsPath {
		s.serveEvents(w, r)
		return
	}

	if s.index != "" && urlPath == "/" {
		if r.URL.Query().Has("raw") {
			http.ServeFile(w, r, s.index)
			return
		}
		s.serveMarkdown(w, s.index, "/"+filepath.Base(s.index), "/?raw")
		return
	}

	fsPath := filepath.Join(s.root, filepath.FromSlash(urlPath))
	info, err := os.Stat(fsPath)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	if info.IsDir() {
		if s.index != "" {
			http.NotFound(w, r)
			return
		}
		if !strings.HasSuffix(r.URL.Path, "/") {
			http.Redirect(w, r, urlPath+"/", http.StatusMovedPermanently)
			return
		}
		s.serveListing(w, r, fsPath, urlPath)
		return
	}

	if !s.allowed(info.Name()) {
		http.NotFound(w, r)
		return
	}
	if render.IsMarkdown(info.Name()) && !r.URL.Query().Has("raw") {
		s.serveMarkdown(w, fsPath, urlPath, urlPath+"?raw")
		return
	}
	http.ServeFile(w, r, fsPath)
}

func (s *Server) allowed(name string) bool {
	if s.exts == nil {
		return true
	}
	e := strings.ToLower(strings.TrimPrefix(filepath.Ext(name), "."))
	return s.exts[e]
}

func (s *Server) serveMarkdown(w http.ResponseWriter, fsPath, urlPath, rawURL string) {
	src, err := os.ReadFile(fsPath)
	if err != nil {
		http.Error(w, "cannot read file", http.StatusInternalServerError)
		return
	}
	res, err := render.Markdown(src)
	if err != nil {
		http.Error(w, "render error: "+err.Error(), http.StatusInternalServerError)
		return
	}
	title := res.Title
	if title == "" {
		title = filepath.Base(fsPath)
	}
	s.render(w, page{
		Title:   title,
		Crumbs:  crumbs(urlPath, s.index == ""),
		Content: template.HTML(res.HTML),
		RawURL:  rawURL,
	})
}

func (s *Server) serveListing(w http.ResponseWriter, r *http.Request, fsPath, urlPath string) {
	dirents, err := os.ReadDir(fsPath)
	if err != nil {
		http.Error(w, "cannot read directory", http.StatusInternalServerError)
		return
	}
	var entries []entry
	for _, d := range dirents {
		name := d.Name()
		if strings.HasPrefix(name, ".") {
			continue
		}
		if !d.IsDir() && !s.allowed(name) {
			continue
		}
		e := entry{Name: name, IsDir: d.IsDir()}
		if d.IsDir() {
			e.URL = path.Join(urlPath, name) + "/"
			e.Name += "/"
		} else {
			e.URL = path.Join(urlPath, name)
			if info, err := d.Info(); err == nil {
				e.Size = humanSize(info.Size())
			}
		}
		entries = append(entries, e)
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].IsDir != entries[j].IsDir {
			return entries[i].IsDir
		}
		return strings.ToLower(entries[i].Name) < strings.ToLower(entries[j].Name)
	})

	var buf bytes.Buffer
	if err := tmpl.ExecuteTemplate(&buf, "listing.html", map[string]any{
		"Entries": entries,
		"Parent":  parentURL(urlPath),
	}); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	title := path.Base(urlPath)
	if urlPath == "/" {
		title = filepath.Base(s.root)
	}
	s.render(w, page{
		Title:   title + "/",
		Crumbs:  crumbs(urlPath, true),
		Content: template.HTML(buf.String()),
	})
}

// serveEvents streams a server-sent "reload" event whenever the file or
// directory behind the "path" query parameter changes.
func (s *Server) serveEvents(w http.ResponseWriter, r *http.Request) {
	if s.hub == nil {
		http.NotFound(w, r)
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	target := s.resolve(r.URL.Query().Get("path"))
	if target == "" {
		http.NotFound(w, r)
		return
	}
	ch, cancel := s.hub.Subscribe(target)
	defer cancel()

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no")
	fmt.Fprint(w, ": connected\n\n")
	flusher.Flush()

	keepalive := time.NewTicker(30 * time.Second)
	defer keepalive.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-ch:
			// Give editors a moment to finish writing before the reload.
			time.Sleep(50 * time.Millisecond)
			fmt.Fprint(w, "event: reload\ndata: 1\n\n")
			flusher.Flush()
			return
		case <-keepalive.C:
			fmt.Fprint(w, ": keepalive\n\n")
			flusher.Flush()
		}
	}
}

// resolve maps a URL path to the filesystem path it is served from, or ""
// if the URL is not something this server would serve.
func (s *Server) resolve(urlPath string) string {
	urlPath = path.Clean("/" + urlPath)
	if hasHiddenSegment(urlPath) {
		return ""
	}
	if s.index != "" {
		if urlPath == "/" {
			return s.index
		}
	}
	fsPath := filepath.Join(s.root, filepath.FromSlash(urlPath))
	if _, err := os.Stat(fsPath); err != nil {
		return ""
	}
	return fsPath
}

func (s *Server) render(w http.ResponseWriter, p page) {
	p.Reload = s.hub != nil
	p.HighlightCSS = template.CSS(render.HighlightCSS())
	var buf bytes.Buffer
	if err := tmpl.ExecuteTemplate(&buf, "page.html", p); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(buf.Bytes())
}

// crumbs builds breadcrumb links for urlPath. When withRoot is false only the
// final element is returned (single-file mode has no navigable root).
func crumbs(urlPath string, withRoot bool) []crumb {
	if !withRoot {
		return []crumb{{Name: path.Base(urlPath)}}
	}
	out := []crumb{{Name: "~", URL: "/"}}
	trimmed := strings.Trim(urlPath, "/")
	if trimmed == "" {
		return out
	}
	acc := ""
	parts := strings.Split(trimmed, "/")
	for i, part := range parts {
		acc += "/" + part
		c := crumb{Name: part, URL: acc + "/"}
		if i == len(parts)-1 {
			c.URL = "" // current location, not a link
		}
		out = append(out, c)
	}
	return out
}

func parentURL(urlPath string) string {
	if urlPath == "/" {
		return ""
	}
	p := path.Dir(strings.TrimSuffix(urlPath, "/"))
	if p == "/" {
		return "/"
	}
	return p + "/"
}

func hasHiddenSegment(p string) bool {
	for _, seg := range strings.Split(p, "/") {
		if strings.HasPrefix(seg, ".") && seg != "." && seg != ".." {
			return true
		}
	}
	return false
}

func humanSize(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for m := n / unit; m >= unit; m /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(n)/float64(div), "KMGTPE"[exp])
}
