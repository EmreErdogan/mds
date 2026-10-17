// Package docx converts markdown to a Word (.docx) document.
package docx

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"fmt"
	"image"
	_ "image/gif" // image formats Word embeds natively
	_ "image/jpeg"
	_ "image/png"
	"net/url"
	"strings"

	"github.com/yuin/goldmark"
	meta "github.com/yuin/goldmark-meta"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	east "github.com/yuin/goldmark/extension/ast"
	"github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"
)

// The parser matches the HTML renderer's, so front matter is dropped.
var md = goldmark.New(goldmark.WithExtensions(extension.GFM, meta.Meta))

const (
	// textWidth is the A4 text width with 1" margins, in twips.
	textWidth = 11906 - 2*1440
	// indent is the indentation per list level, in twips.
	indent = 720
	// emuPerPx converts pixels at 96 dpi to EMUs.
	emuPerPx = 9525
	// bulletNum is the numbering instance shared by all bullet lists.
	bulletNum = 1
)

// ImageLoader returns the contents of the image an image destination points
// at, or false if it is not available.
type ImageLoader func(dest string) ([]byte, bool)

// Diagram is a picture of a diagram code block drawn elsewhere: mermaid
// only renders in a browser.
type Diagram struct {
	PNG           []byte // shown by every reader
	SVG           []byte // optional; Word 2016 and later prefer it
	Width, Height int    // display size in CSS pixels
}

// Options configures Convert.
type Options struct {
	// Title goes into the document properties.
	Title string
	// Images loads image destinations. Images it cannot provide, or that are
	// not PNG, JPEG or GIF, are replaced by their alt text. May be nil.
	Images ImageLoader
	// Diagrams maps the trimmed source of a mermaid code block to its
	// picture. Blocks without one stay code.
	Diagrams map[string]Diagram
}

// Convert renders markdown src as a .docx file.
func Convert(src []byte, opts Options) ([]byte, error) {
	w := &writer{src: src, load: opts.Images, diagrams: opts.Diagrams}
	w.blocks(md.Parser().Parse(text.NewReader(src)), ctx{})
	return w.pack(opts.Title)
}

type rel struct {
	id, typ, target string
	external        bool
}

type media struct {
	name string
	data []byte
}

// orderedNum is a numbering instance that restarts an ordered list.
type orderedNum struct {
	level, start int
}

type writer struct {
	src      []byte
	load     ImageLoader
	diagrams map[string]Diagram
	b        bytes.Buffer // document body
	rels     []rel
	media    []media
	pictures int
	nums     []orderedNum
}

// ctx is where a block sits.
type ctx struct {
	quote bool
	level int   // list nesting depth, 0 outside lists
	numID int   // numbering of the innermost list
	item  *bool // the list item's first paragraph, which carries the number, is still to come
}

// run is the character formatting in effect.
type run struct {
	bold, italic, strike, code, link bool
}

func (w *writer) blocks(parent ast.Node, c ctx) {
	for n := parent.FirstChild(); n != nil; n = n.NextSibling() {
		w.block(n, c)
	}
}

func (w *writer) block(n ast.Node, c ctx) {
	switch n := n.(type) {
	case *ast.Heading:
		w.para(n, &c, fmt.Sprintf("Heading%d", n.Level))
	case *ast.Paragraph, *ast.TextBlock:
		w.para(n, &c, "")
	case *ast.ThematicBreak:
		w.b.WriteString(`<w:p><w:pPr><w:pBdr><w:bottom w:val="single" w:sz="6" w:space="1" w:color="D0D7DE"/></w:pBdr></w:pPr></w:p>`)
	case *ast.FencedCodeBlock, *ast.CodeBlock:
		w.code(n, &c)
	case *ast.Blockquote:
		c.quote = true
		w.blocks(n, c)
	case *ast.List:
		w.list(n, c)
	case *east.Table:
		w.table(n)
	case *ast.HTMLBlock:
		// Raw HTML has no Word equivalent.
	default:
		w.blocks(n, c)
	}
}

func (w *writer) para(n ast.Node, c *ctx, style string) {
	w.b.WriteString("<w:p>")
	w.pPr(c, style)
	w.inlines(n, run{})
	w.b.WriteString("</w:p>")
}

// pPr writes paragraph properties for a paragraph in c. The first paragraph
// of a list item gets the item's number; later ones are indented to match.
func (w *writer) pPr(c *ctx, style string) {
	if style == "" {
		switch {
		case c.quote:
			style = "Quote"
		case c.level > 0:
			style = "ListParagraph"
		}
	}
	w.b.WriteString("<w:pPr>")
	if style != "" {
		fmt.Fprintf(&w.b, `<w:pStyle w:val="%s"/>`, style)
	}
	switch {
	case c.item != nil && *c.item:
		*c.item = false
		fmt.Fprintf(&w.b, `<w:numPr><w:ilvl w:val="%d"/><w:numId w:val="%d"/></w:numPr>`, min(c.level, 9)-1, c.numID)
	case c.level > 0:
		fmt.Fprintf(&w.b, `<w:ind w:left="%d"/>`, c.level*indent)
	}
	w.b.WriteString("</w:pPr>")
}

// code writes a code block as one paragraph with a line break per line, so
// its shading stays continuous.
func (w *writer) code(n ast.Node, c *ctx) {
	var lines []string
	for i := 0; i < n.Lines().Len(); i++ {
		seg := n.Lines().At(i)
		lines = append(lines, strings.TrimRight(string(seg.Value(w.src)), "\r\n"))
	}
	if f, ok := n.(*ast.FencedCodeBlock); ok && string(f.Language(w.src)) == "mermaid" && w.diagram(lines, c) {
		return
	}
	w.b.WriteString("<w:p>")
	w.pPr(c, "Code")
	for i, line := range lines {
		w.b.WriteString("<w:r>")
		if i > 0 {
			w.b.WriteString("<w:br/>")
		}
		for j, part := range strings.Split(line, "\t") {
			if j > 0 {
				w.b.WriteString("<w:tab/>")
			}
			if part != "" {
				w.b.WriteString(`<w:t xml:space="preserve">`)
				w.b.WriteString(esc(part))
				w.b.WriteString("</w:t>")
			}
		}
		w.b.WriteString("</w:r>")
	}
	w.b.WriteString("</w:p>")
}

// diagram writes the picture of the mermaid source in lines, if there is one.
func (w *writer) diagram(lines []string, c *ctx) bool {
	d, ok := w.diagrams[strings.TrimSpace(strings.Join(lines, "\n"))]
	if !ok {
		return false
	}
	cfg, format, err := image.DecodeConfig(bytes.NewReader(d.PNG))
	if err != nil || format != "png" {
		return false
	}
	width, height := d.Width, d.Height
	if width <= 0 || height <= 0 {
		width, height = cfg.Width, cfg.Height
	}
	w.b.WriteString("<w:p>")
	w.pPr(c, "Diagram")
	w.drawing(d.PNG, format, width, height, "Diagram", d.SVG)
	w.b.WriteString("</w:p>")
	return true
}

func (w *writer) list(l *ast.List, c ctx) {
	c.level++
	c.numID = bulletNum
	if l.IsOrdered() {
		w.nums = append(w.nums, orderedNum{level: min(c.level, 9) - 1, start: l.Start})
		c.numID = bulletNum + len(w.nums)
	}
	for item := l.FirstChild(); item != nil; item = item.NextSibling() {
		first := true
		c.item = &first
		w.blocks(item, c)
	}
}

func (w *writer) table(t *east.Table) {
	cols := len(t.Alignments)
	if cols == 0 {
		return
	}
	w.b.WriteString(`<w:tbl><w:tblPr><w:tblStyle w:val="Table"/><w:tblW w:w="5000" w:type="pct"/></w:tblPr><w:tblGrid>`)
	for i := 0; i < cols; i++ {
		fmt.Fprintf(&w.b, `<w:gridCol w:w="%d"/>`, textWidth/cols)
	}
	w.b.WriteString("</w:tblGrid>")
	for row := t.FirstChild(); row != nil; row = row.NextSibling() {
		_, header := row.(*east.TableHeader)
		w.b.WriteString("<w:tr>")
		if header {
			w.b.WriteString("<w:trPr><w:tblHeader/></w:trPr>")
		}
		for cell := row.FirstChild(); cell != nil; cell = cell.NextSibling() {
			w.b.WriteString(`<w:tc><w:tcPr><w:tcW w:w="0" w:type="auto"/>`)
			if header {
				w.b.WriteString(`<w:shd w:val="clear" w:color="auto" w:fill="F6F8FA"/>`)
			}
			w.b.WriteString(`</w:tcPr><w:p><w:pPr><w:spacing w:after="0"/>`)
			if tc, ok := cell.(*east.TableCell); ok {
				switch tc.Alignment {
				case east.AlignLeft:
					w.b.WriteString(`<w:jc w:val="left"/>`)
				case east.AlignCenter:
					w.b.WriteString(`<w:jc w:val="center"/>`)
				case east.AlignRight:
					w.b.WriteString(`<w:jc w:val="right"/>`)
				}
			}
			w.b.WriteString("</w:pPr>")
			w.inlines(cell, run{bold: header})
			w.b.WriteString("</w:p></w:tc>")
		}
		w.b.WriteString("</w:tr>")
	}
	w.b.WriteString("</w:tbl>")
}

func (w *writer) inlines(parent ast.Node, r run) {
	for n := parent.FirstChild(); n != nil; n = n.NextSibling() {
		w.inline(n, r)
	}
}

func (w *writer) inline(n ast.Node, r run) {
	switch n := n.(type) {
	case *ast.Text:
		v := n.Segment.Value(w.src)
		if !n.IsRaw() {
			v = util.ResolveEntityNames(util.ResolveNumericReferences(util.UnescapePunctuations(v)))
		}
		w.text(r, string(v))
		switch {
		case n.HardLineBreak():
			w.b.WriteString("<w:r><w:br/></w:r>")
		case n.SoftLineBreak():
			w.text(r, " ")
		}
	case *ast.String:
		w.text(r, string(n.Value))
	case *ast.CodeSpan:
		r.code = true
		w.inlines(n, r)
	case *ast.Emphasis:
		if n.Level >= 2 {
			r.bold = true
		} else {
			r.italic = true
		}
		w.inlines(n, r)
	case *east.Strikethrough:
		r.strike = true
		w.inlines(n, r)
	case *ast.Link:
		w.link(string(n.Destination), r, func(r run) { w.inlines(n, r) })
	case *ast.AutoLink:
		dest, label := string(n.URL(w.src)), string(n.Label(w.src))
		if n.AutoLinkType == ast.AutoLinkEmail && !strings.HasPrefix(strings.ToLower(dest), "mailto:") {
			dest = "mailto:" + dest
		}
		w.link(dest, r, func(r run) { w.text(r, label) })
	case *ast.Image:
		w.image(n, r)
	case *east.TaskCheckBox:
		if n.IsChecked {
			w.text(r, "☒ ")
		} else {
			w.text(r, "☐ ")
		}
	case *ast.RawHTML:
		var raw strings.Builder
		for i := 0; i < n.Segments.Len(); i++ {
			seg := n.Segments.At(i)
			raw.Write(seg.Value(w.src))
		}
		switch strings.ToLower(strings.ReplaceAll(raw.String(), " ", "")) {
		case "<br>", "<br/>":
			w.b.WriteString("<w:r><w:br/></w:r>")
		}
	default:
		w.inlines(n, r)
	}
}

// link writes body as a hyperlink to dest. Relative links have nowhere to
// point inside a .docx, so they become plain text.
func (w *writer) link(dest string, r run, body func(run)) {
	u, err := url.Parse(dest)
	if err != nil {
		body(r)
		return
	}
	switch strings.ToLower(u.Scheme) {
	case "http", "https", "mailto", "ftp":
	default:
		body(r)
		return
	}
	fmt.Fprintf(&w.b, `<w:hyperlink r:id="%s">`, w.rel("hyperlink", dest, true))
	r.link = true
	body(r)
	w.b.WriteString("</w:hyperlink>")
}

func (w *writer) text(r run, s string) {
	if s == "" {
		return
	}
	w.b.WriteString("<w:r>")
	var p strings.Builder
	switch {
	case r.link:
		p.WriteString(`<w:rStyle w:val="Hyperlink"/>`)
		if r.code {
			p.WriteString(`<w:rFonts w:ascii="Consolas" w:hAnsi="Consolas" w:cs="Consolas"/>`)
		}
	case r.code:
		p.WriteString(`<w:rStyle w:val="CodeChar"/>`)
	}
	if r.bold {
		p.WriteString("<w:b/>")
	}
	if r.italic {
		p.WriteString("<w:i/>")
	}
	if r.strike {
		p.WriteString("<w:strike/>")
	}
	if p.Len() > 0 {
		w.b.WriteString("<w:rPr>" + p.String() + "</w:rPr>")
	}
	w.b.WriteString(`<w:t xml:space="preserve">`)
	w.b.WriteString(esc(s))
	w.b.WriteString("</w:t></w:r>")
}

func (w *writer) image(n *ast.Image, r run) {
	alt := plain(n, w.src)
	if w.load != nil {
		if data, ok := w.load(string(n.Destination)); ok {
			cfg, format, err := image.DecodeConfig(bytes.NewReader(data))
			if err == nil && cfg.Width > 0 && cfg.Height > 0 {
				w.drawing(data, format, cfg.Width, cfg.Height, alt, nil)
				return
			}
		}
	}
	r.italic = true
	w.text(r, alt)
}

// drawing writes an inline picture, scaled down to the text width if wider.
// An SVG version, if given, is attached the way Word does it: readers that
// support it show the SVG, the rest the bitmap.
func (w *writer) drawing(data []byte, format string, width, height int, alt string, svg []byte) {
	const maxCX = textWidth * 635 // twips to EMUs
	cx, cy := int64(width)*emuPerPx, int64(height)*emuPerPx
	if cx > maxCX {
		cx, cy = maxCX, cy*maxCX/cx
	}
	w.pictures++
	n := w.pictures
	name := fmt.Sprintf("image%d.%s", n, format)
	w.media = append(w.media, media{name: name, data: data})
	id := w.rel("image", "media/"+name, false)
	blip := fmt.Sprintf(`<a:blip r:embed="%s"/>`, id)
	if svg != nil {
		svgName := fmt.Sprintf("image%d.svg", n)
		w.media = append(w.media, media{name: svgName, data: svg})
		blip = fmt.Sprintf(`<a:blip r:embed="%s"><a:extLst><a:ext uri="{96DAC541-7B7A-43D3-8B79-37D633B846F1}">`+
			`<asvg:svgBlip xmlns:asvg="http://schemas.microsoft.com/office/drawing/2016/SVG/main" r:embed="%s"/>`+
			`</a:ext></a:extLst></a:blip>`, id, w.rel("image", "media/"+svgName, false))
	}
	fmt.Fprintf(&w.b, `<w:r><w:drawing><wp:inline distT="0" distB="0" distL="0" distR="0">`+
		`<wp:extent cx="%d" cy="%d"/><wp:docPr id="%d" name="Picture %d" descr="%s"/>`+
		`<a:graphic><a:graphicData uri="http://schemas.openxmlformats.org/drawingml/2006/picture"><pic:pic>`+
		`<pic:nvPicPr><pic:cNvPr id="%d" name="%s"/><pic:cNvPicPr/></pic:nvPicPr>`+
		`<pic:blipFill>%s<a:stretch><a:fillRect/></a:stretch></pic:blipFill>`+
		`<pic:spPr><a:xfrm><a:off x="0" y="0"/><a:ext cx="%d" cy="%d"/></a:xfrm><a:prstGeom prst="rect"><a:avLst/></a:prstGeom></pic:spPr>`+
		`</pic:pic></a:graphicData></a:graphic></wp:inline></w:drawing></w:r>`,
		cx, cy, n, n, esc(alt), n, name, blip, cx, cy)
}

// rel adds a relationship from the document part and returns its id. Ids
// rId1 and rId2 belong to the styles and numbering parts.
func (w *writer) rel(typ, target string, external bool) string {
	id := fmt.Sprintf("rId%d", len(w.rels)+3)
	w.rels = append(w.rels, rel{id: id, typ: typ, target: target, external: external})
	return id
}

func plain(n ast.Node, src []byte) string {
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

func esc(s string) string {
	var b strings.Builder
	_ = xml.EscapeText(&b, []byte(s))
	return b.String()
}

const (
	xmlHeader  = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` + "\n"
	nsW        = "http://schemas.openxmlformats.org/wordprocessingml/2006/main"
	nsRelTypes = "http://schemas.openxmlformats.org/officeDocument/2006/relationships/"
)

func (w *writer) pack(title string) ([]byte, error) {
	var doc strings.Builder
	doc.WriteString(xmlHeader)
	doc.WriteString(`<w:document xmlns:w="` + nsW + `" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"` +
		` xmlns:wp="http://schemas.openxmlformats.org/drawingml/2006/wordprocessingDrawing"` +
		` xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main"` +
		` xmlns:pic="http://schemas.openxmlformats.org/drawingml/2006/picture"><w:body>`)
	doc.Write(w.b.Bytes())
	if bytes.HasSuffix(w.b.Bytes(), []byte("</w:tbl>")) {
		// Word expects the body to end with a paragraph.
		doc.WriteString("<w:p/>")
	}
	doc.WriteString(`<w:sectPr><w:pgSz w:w="11906" w:h="16838"/>` +
		`<w:pgMar w:top="1440" w:right="1440" w:bottom="1440" w:left="1440" w:header="708" w:footer="708" w:gutter="0"/>` +
		`</w:sectPr></w:body></w:document>`)

	var rels strings.Builder
	rels.WriteString(xmlHeader + `<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">` +
		`<Relationship Id="rId1" Type="` + nsRelTypes + `styles" Target="styles.xml"/>` +
		`<Relationship Id="rId2" Type="` + nsRelTypes + `numbering" Target="numbering.xml"/>`)
	for _, r := range w.rels {
		mode := ""
		if r.external {
			mode = ` TargetMode="External"`
		}
		fmt.Fprintf(&rels, `<Relationship Id="%s" Type="%s%s" Target="%s"%s/>`, r.id, nsRelTypes, r.typ, esc(r.target), mode)
	}
	rels.WriteString("</Relationships>")

	parts := []struct{ name, body string }{
		{"[Content_Types].xml", contentTypes},
		{"_rels/.rels", packageRels},
		{"docProps/core.xml", xmlHeader + `<cp:coreProperties xmlns:cp="http://schemas.openxmlformats.org/package/2006/metadata/core-properties"` +
			` xmlns:dc="http://purl.org/dc/elements/1.1/"><dc:title>` + esc(title) + `</dc:title></cp:coreProperties>`},
		{"word/document.xml", doc.String()},
		{"word/_rels/document.xml.rels", rels.String()},
		{"word/styles.xml", styles},
		{"word/numbering.xml", w.numbering()},
	}
	var buf bytes.Buffer
	z := zip.NewWriter(&buf)
	for _, p := range parts {
		f, err := z.Create(p.name)
		if err != nil {
			return nil, err
		}
		if _, err := f.Write([]byte(p.body)); err != nil {
			return nil, err
		}
	}
	for _, m := range w.media {
		f, err := z.Create("word/media/" + m.name)
		if err != nil {
			return nil, err
		}
		if _, err := f.Write(m.data); err != nil {
			return nil, err
		}
	}
	if err := z.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// numbering has one abstract numbering for bullets and one for ordered
// lists. Every ordered list gets its own instance so that it restarts at its
// own start number.
func (w *writer) numbering() string {
	var b strings.Builder
	b.WriteString(xmlHeader + `<w:numbering xmlns:w="` + nsW + `">`)
	bullets := []string{"•", "◦", "▪"}
	for id, format := range []string{"bullet", "decimal"} {
		fmt.Fprintf(&b, `<w:abstractNum w:abstractNumId="%d"><w:multiLevelType w:val="hybridMultilevel"/>`, id)
		for l := 0; l < 9; l++ {
			lvlText := fmt.Sprintf("%%%d.", l+1)
			if format == "bullet" {
				lvlText = bullets[l%len(bullets)]
			}
			fmt.Fprintf(&b, `<w:lvl w:ilvl="%d"><w:start w:val="1"/><w:numFmt w:val="%s"/><w:lvlText w:val="%s"/>`+
				`<w:lvlJc w:val="left"/><w:pPr><w:ind w:left="%d" w:hanging="360"/></w:pPr></w:lvl>`,
				l, format, lvlText, (l+1)*indent)
		}
		b.WriteString("</w:abstractNum>")
	}
	fmt.Fprintf(&b, `<w:num w:numId="%d"><w:abstractNumId w:val="0"/></w:num>`, bulletNum)
	for i, n := range w.nums {
		fmt.Fprintf(&b, `<w:num w:numId="%d"><w:abstractNumId w:val="1"/>`+
			`<w:lvlOverride w:ilvl="%d"><w:startOverride w:val="%d"/></w:lvlOverride></w:num>`,
			bulletNum+i+1, n.level, n.start)
	}
	b.WriteString("</w:numbering>")
	return b.String()
}

const contentTypes = xmlHeader + `<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">` +
	`<Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/>` +
	`<Default Extension="xml" ContentType="application/xml"/>` +
	`<Default Extension="png" ContentType="image/png"/>` +
	`<Default Extension="jpeg" ContentType="image/jpeg"/>` +
	`<Default Extension="gif" ContentType="image/gif"/>` +
	`<Default Extension="svg" ContentType="image/svg+xml"/>` +
	`<Override PartName="/word/document.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml"/>` +
	`<Override PartName="/word/styles.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.styles+xml"/>` +
	`<Override PartName="/word/numbering.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.numbering+xml"/>` +
	`<Override PartName="/docProps/core.xml" ContentType="application/vnd.openxmlformats-package.core-properties+xml"/>` +
	`</Types>`

const packageRels = xmlHeader + `<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">` +
	`<Relationship Id="rId1" Type="` + nsRelTypes + `officeDocument" Target="word/document.xml"/>` +
	`<Relationship Id="rId2" Type="http://schemas.openxmlformats.org/package/2006/relationships/metadata/core-properties" Target="docProps/core.xml"/>` +
	`</Relationships>`

var styles = xmlHeader + `<w:styles xmlns:w="` + nsW + `">` +
	`<w:docDefaults><w:rPrDefault><w:rPr><w:rFonts w:ascii="Calibri" w:hAnsi="Calibri" w:eastAsia="Calibri" w:cs="Calibri"/>` +
	`<w:sz w:val="22"/><w:szCs w:val="22"/></w:rPr></w:rPrDefault>` +
	`<w:pPrDefault><w:pPr><w:spacing w:after="160" w:line="276" w:lineRule="auto"/></w:pPr></w:pPrDefault></w:docDefaults>` +
	`<w:style w:type="paragraph" w:default="1" w:styleId="Normal"><w:name w:val="Normal"/><w:qFormat/></w:style>` +
	headingStyles() +
	`<w:style w:type="paragraph" w:styleId="ListParagraph"><w:name w:val="List Paragraph"/><w:basedOn w:val="Normal"/><w:qFormat/>` +
	`<w:pPr><w:contextualSpacing/></w:pPr></w:style>` +
	`<w:style w:type="paragraph" w:styleId="Quote"><w:name w:val="Quote"/><w:basedOn w:val="Normal"/><w:qFormat/>` +
	`<w:pPr><w:pBdr><w:left w:val="single" w:sz="18" w:space="8" w:color="D0D7DE"/></w:pBdr><w:ind w:left="240"/></w:pPr>` +
	`<w:rPr><w:color w:val="57606A"/></w:rPr></w:style>` +
	`<w:style w:type="paragraph" w:styleId="Code"><w:name w:val="Code"/><w:basedOn w:val="Normal"/>` +
	`<w:pPr><w:shd w:val="clear" w:color="auto" w:fill="F6F8FA"/><w:spacing w:line="240" w:lineRule="auto"/></w:pPr>` +
	`<w:rPr><w:rFonts w:ascii="Consolas" w:hAnsi="Consolas" w:cs="Consolas"/><w:sz w:val="19"/><w:szCs w:val="19"/></w:rPr></w:style>` +
	`<w:style w:type="paragraph" w:styleId="Diagram"><w:name w:val="Diagram"/><w:basedOn w:val="Normal"/>` +
	`<w:pPr><w:keepLines/><w:jc w:val="center"/></w:pPr></w:style>` +
	`<w:style w:type="character" w:styleId="CodeChar"><w:name w:val="Inline Code"/>` +
	`<w:rPr><w:rFonts w:ascii="Consolas" w:hAnsi="Consolas" w:cs="Consolas"/><w:sz w:val="20"/><w:szCs w:val="20"/>` +
	`<w:shd w:val="clear" w:color="auto" w:fill="EFF1F3"/></w:rPr></w:style>` +
	`<w:style w:type="character" w:styleId="Hyperlink"><w:name w:val="Hyperlink"/>` +
	`<w:rPr><w:color w:val="0969DA"/><w:u w:val="single"/></w:rPr></w:style>` +
	`<w:style w:type="table" w:styleId="Table"><w:name w:val="Markdown Table"/><w:tblPr><w:tblBorders>` +
	`<w:top w:val="single" w:sz="4" w:space="0" w:color="D0D7DE"/><w:left w:val="single" w:sz="4" w:space="0" w:color="D0D7DE"/>` +
	`<w:bottom w:val="single" w:sz="4" w:space="0" w:color="D0D7DE"/><w:right w:val="single" w:sz="4" w:space="0" w:color="D0D7DE"/>` +
	`<w:insideH w:val="single" w:sz="4" w:space="0" w:color="D0D7DE"/><w:insideV w:val="single" w:sz="4" w:space="0" w:color="D0D7DE"/>` +
	`</w:tblBorders><w:tblCellMar><w:top w:w="60" w:type="dxa"/><w:left w:w="100" w:type="dxa"/>` +
	`<w:bottom w:w="60" w:type="dxa"/><w:right w:w="100" w:type="dxa"/></w:tblCellMar></w:tblPr></w:style>` +
	`</w:styles>`

func headingStyles() string {
	var b strings.Builder
	sizes := []int{40, 32, 28, 24, 22, 22} // half-points
	for i, sz := range sizes {
		fmt.Fprintf(&b, `<w:style w:type="paragraph" w:styleId="Heading%d"><w:name w:val="heading %d"/>`+
			`<w:basedOn w:val="Normal"/><w:next w:val="Normal"/><w:qFormat/>`+
			`<w:pPr><w:keepNext/><w:keepLines/><w:spacing w:before="%d" w:after="120"/><w:outlineLvl w:val="%d"/></w:pPr>`+
			`<w:rPr><w:b/><w:bCs/><w:sz w:val="%d"/><w:szCs w:val="%d"/></w:rPr></w:style>`,
			i+1, i+1, 360-i*40, i, sz, sz)
	}
	return b.String()
}
