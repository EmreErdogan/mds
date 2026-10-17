package server

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"html"
	"html/template"
	"math"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path"
	"regexp"
	"strings"

	"github.com/EmreErdogan/mds/internal/docx"
	"github.com/EmreErdogan/mds/internal/render"
)

// download sends the markdown page at urlPath as a file in format: "md" (the
// source), "html" (a standalone page with local images inlined) or "docx".
// PDF is left to the browser's print dialog. The page posts its mermaid
// diagrams along, drawn as pictures; without them diagrams stay code.
func (s *Server) download(w http.ResponseWriter, r *http.Request, src []byte, name, urlPath, format string) {
	load := func(dest string) ([]byte, bool) { return s.localFile(urlPath, dest) }
	diagrams := postedDiagrams(w, r)
	var body []byte
	var ctype string
	switch format {
	case "md":
		body, ctype = src, "text/markdown; charset=utf-8"
	case "html":
		res, err := render.Markdown(src, render.WithImages(func(dest string) string {
			data, ok := load(dest)
			if !ok {
				return ""
			}
			t := mime.TypeByExtension(path.Ext(dest))
			if t == "" {
				t = http.DetectContentType(data)
			}
			if !strings.HasPrefix(t, "image/") {
				return ""
			}
			return "data:" + t + ";base64," + base64.StdEncoding.EncodeToString(data)
		}))
		if err != nil {
			http.Error(w, "render error: "+err.Error(), http.StatusInternalServerError)
			return
		}
		out := inlineDiagrams(res.HTML, diagrams)
		var buf bytes.Buffer
		if err := tmpl.ExecuteTemplate(&buf, "export.html", s.themed(page{
			Title:        pageTitle(res, name),
			Content:      template.HTML(out),
			Mermaid:      bytes.Contains(out, []byte(`class="language-mermaid"`)),
			HighlightCSS: template.CSS(render.HighlightCSS()),
		})); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		body, ctype = buf.Bytes(), "text/html; charset=utf-8"
	case "docx":
		res, err := render.Markdown(src)
		if err != nil {
			http.Error(w, "render error: "+err.Error(), http.StatusInternalServerError)
			return
		}
		body, err = docx.Convert(src, docx.Options{Title: pageTitle(res, name), Images: load, Diagrams: diagrams})
		if err != nil {
			http.Error(w, "docx error: "+err.Error(), http.StatusInternalServerError)
			return
		}
		ctype = "application/vnd.openxmlformats-officedocument.wordprocessingml.document"
	default:
		http.Error(w, "unknown download format", http.StatusBadRequest)
		return
	}
	disp := mime.FormatMediaType("attachment", map[string]string{"filename": downloadName(name, format)})
	if disp == "" {
		disp = "attachment"
	}
	w.Header().Set("Content-Type", ctype)
	w.Header().Set("Content-Disposition", disp)
	_, _ = w.Write(body)
}

// maxPostSize caps the diagrams a download request may carry.
const maxPostSize = 64 << 20

// postedDiagrams reads the diagrams posted with a download request, keyed by
// trimmed mermaid source. The "diagrams" form field holds a JSON list of
// {src, svg, png (base64), width, height}.
func postedDiagrams(w http.ResponseWriter, r *http.Request) map[string]docx.Diagram {
	if r.Method != http.MethodPost {
		return nil
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxPostSize)
	var list []struct {
		Src, SVG, PNG string
		Width, Height float64
	}
	if err := json.Unmarshal([]byte(r.FormValue("diagrams")), &list); err != nil {
		return nil
	}
	out := make(map[string]docx.Diagram, len(list))
	for _, d := range list {
		png, _ := base64.StdEncoding.DecodeString(d.PNG)
		out[strings.TrimSpace(d.Src)] = docx.Diagram{
			PNG:    png,
			SVG:    []byte(d.SVG),
			Width:  int(math.Round(d.Width)),
			Height: int(math.Round(d.Height)),
		}
	}
	return out
}

var mermaidBlock = regexp.MustCompile(`(?s)<pre><code class="language-mermaid">(.*?)</code></pre>`)

// inlineDiagrams replaces rendered mermaid code blocks that have a picture
// with that picture as an SVG image.
func inlineDiagrams(out []byte, diagrams map[string]docx.Diagram) []byte {
	if len(diagrams) == 0 {
		return out
	}
	return mermaidBlock.ReplaceAllFunc(out, func(m []byte) []byte {
		src := html.UnescapeString(string(mermaidBlock.FindSubmatch(m)[1]))
		d, ok := diagrams[strings.TrimSpace(src)]
		if !ok || len(d.SVG) == 0 {
			return m
		}
		size := ""
		if d.Width > 0 && d.Height > 0 {
			size = fmt.Sprintf(` width="%d" height="%d"`, d.Width, d.Height)
		}
		return fmt.Appendf(nil, `<p class="diagram"><img src="data:image/svg+xml;base64,%s"%s alt="Diagram"></p>`,
			base64.StdEncoding.EncodeToString(d.SVG), size)
	})
}

// downloadName is the file name for name downloaded as format.
func downloadName(name, format string) string {
	if format == "md" && render.IsMarkdown(name) {
		return name
	}
	base := strings.TrimSuffix(name, path.Ext(name))
	if base == "" {
		base = "document"
	}
	return base + "." + format
}

// localFile returns the contents of the file that dest, a link destination
// in the markdown page at urlPath, points at, if this server would serve it.
func (s *Server) localFile(urlPath, dest string) ([]byte, bool) {
	if s.content != nil {
		return nil, false
	}
	u, err := url.Parse(dest)
	if err != nil || u.Scheme != "" || u.Host != "" || u.Path == "" {
		return nil, false
	}
	p := u.Path
	if !strings.HasPrefix(p, "/") {
		p = path.Join(path.Dir(urlPath), p)
	}
	fsPath := s.resolve(p)
	if fsPath == "" || !s.allowedType(fsPath) {
		return nil, false
	}
	if info, err := os.Stat(fsPath); err != nil || !info.Mode().IsRegular() {
		return nil, false
	}
	data, err := os.ReadFile(fsPath)
	return data, err == nil
}
