package docx

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"image"
	"image/png"
	"io"
	"strings"
	"testing"
)

// unpack returns the parts of a .docx, checking that every XML part is well
// formed.
func unpack(t *testing.T, data []byte) map[string]string {
	t.Helper()
	z, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	parts := map[string]string{}
	for _, f := range z.File {
		rc, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		b, _ := io.ReadAll(rc)
		rc.Close()
		parts[f.Name] = string(b)
		if strings.HasSuffix(f.Name, ".xml") || strings.HasSuffix(f.Name, ".rels") {
			d := xml.NewDecoder(bytes.NewReader(b))
			for {
				if _, err := d.Token(); err == io.EOF {
					break
				} else if err != nil {
					t.Fatalf("%s: %v", f.Name, err)
				}
			}
		}
	}
	return parts
}

func TestConvert(t *testing.T) {
	src := "---\ntitle: Meta\n---\n# Title & co\n\n" +
		"Some **bold**, *italic*, ~~gone~~, `code` and a [link](https://example.com/?a=1&b=2), " +
		"a [relative](other.md) one and <https://auto.example>.  \nAfter break \\*literal\\* &copy;\n\n" +
		"- one\n- two\n  - nested\n\n1. first\n2. second\n\nbetween\n\n3. restart\n4. at three\n\n" +
		"> quoted\n\n```go\nfunc main() {\n\tfmt.Println(\"<hi>\")\n}\n```\n\n---\n\n" +
		"- [x] done\n- [ ] todo\n\n| a | b |\n|:-:|--:|\n| 1 | **2** |\n\n<div>raw</div>\n"
	data, err := Convert([]byte(src), Options{Title: "Doc Title"})
	if err != nil {
		t.Fatal(err)
	}
	parts := unpack(t, data)
	for _, name := range []string{"[Content_Types].xml", "_rels/.rels", "word/document.xml", "word/styles.xml", "word/numbering.xml", "word/_rels/document.xml.rels", "docProps/core.xml"} {
		if _, ok := parts[name]; !ok {
			t.Errorf("missing part %s", name)
		}
	}
	doc := parts["word/document.xml"]
	for _, want := range []string{
		`<w:pStyle w:val="Heading1"/>`, `Title &amp;`,
		`<w:b/></w:rPr><w:t xml:space="preserve">bold`, `<w:i/></w:rPr><w:t xml:space="preserve">italic`,
		`<w:strike/>`, `<w:rStyle w:val="CodeChar"/>`, `<w:rStyle w:val="Hyperlink"/>`,
		`<w:t xml:space="preserve">relative</w:t>`, `<w:r><w:br/></w:r>`, `*literal*`, `©`,
		`<w:pStyle w:val="Quote"/>`, `<w:pStyle w:val="Code"/>`, `<w:tab/>`, `fmt.Println(&#34;&lt;hi&gt;&#34;)`,
		`☒ `, `☐ `, `<w:tbl>`, `<w:jc w:val="center"/>`, `<w:jc w:val="right"/>`, `<w:tblHeader/>`,
		`<w:ilvl w:val="1"/>`,
	} {
		if !strings.Contains(doc, want) {
			t.Errorf("document.xml missing %q", want)
		}
	}
	for _, unwanted := range []string{"title: Meta", ">raw<", `\*`} {
		if strings.Contains(doc, unwanted) {
			t.Errorf("document.xml contains %q", unwanted)
		}
	}
	if !strings.HasSuffix(doc, `<w:p/><w:sectPr><w:pgSz w:w="11906" w:h="16838"/><w:pgMar w:top="1440" w:right="1440" w:bottom="1440" w:left="1440" w:header="708" w:footer="708" w:gutter="0"/></w:sectPr></w:body></w:document>`) {
		t.Error("body ending in a table should get a closing paragraph")
	}
	rels := parts["word/_rels/document.xml.rels"]
	if !strings.Contains(rels, `Target="https://example.com/?a=1&amp;b=2" TargetMode="External"`) || !strings.Contains(rels, `Target="https://auto.example"`) {
		t.Errorf("hyperlink relationships: %s", rels)
	}
	if strings.Contains(rels, "other.md") {
		t.Error("relative link became a hyperlink")
	}
	// Two ordered lists, each with its own restartable numbering.
	num := parts["word/numbering.xml"]
	if !strings.Contains(num, `<w:num w:numId="2"><w:abstractNumId w:val="1"/><w:lvlOverride w:ilvl="0"><w:startOverride w:val="1"/>`) ||
		!strings.Contains(num, `<w:num w:numId="3"><w:abstractNumId w:val="1"/><w:lvlOverride w:ilvl="0"><w:startOverride w:val="3"/>`) {
		t.Errorf("numbering: %s", num)
	}
	if !strings.Contains(parts["docProps/core.xml"], "<dc:title>Doc Title</dc:title>") {
		t.Error("title not in core properties")
	}
}

func TestImages(t *testing.T) {
	var pngData bytes.Buffer
	if err := png.Encode(&pngData, image.NewRGBA(image.Rect(0, 0, 2000, 100))); err != nil {
		t.Fatal(err)
	}
	load := func(dest string) ([]byte, bool) {
		switch dest {
		case "wide.png":
			return pngData.Bytes(), true
		case "notimage.png":
			return []byte("text"), true
		}
		return nil, false
	}
	src := "![Wide \"one\"](wide.png) ![broken](notimage.png) ![missing](nope.png)\n"
	data, err := Convert([]byte(src), Options{Images: load})
	if err != nil {
		t.Fatal(err)
	}
	parts := unpack(t, data)
	if _, ok := parts["word/media/image1.png"]; !ok {
		t.Fatal("image not embedded")
	}
	if len(parts) != 8 {
		t.Errorf("got %d parts, want 8 (one image)", len(parts))
	}
	doc := parts["word/document.xml"]
	// 2000px is wider than the text width, so it is scaled down to it.
	if !strings.Contains(doc, `<wp:extent cx="5731510" cy="286575"/>`) || !strings.Contains(doc, `descr="Wide &#34;one&#34;"`) {
		t.Errorf("drawing: %s", doc)
	}
	if !strings.Contains(doc, `<w:i/></w:rPr><w:t xml:space="preserve">broken</w:t>`) || !strings.Contains(doc, ">missing</w:t>") {
		t.Error("unavailable images should become alt text")
	}
	if !strings.Contains(parts["word/_rels/document.xml.rels"], `Target="media/image1.png"`) {
		t.Error("image relationship missing")
	}
}

func TestDiagrams(t *testing.T) {
	var pngData bytes.Buffer
	if err := png.Encode(&pngData, image.NewRGBA(image.Rect(0, 0, 200, 100))); err != nil {
		t.Fatal(err)
	}
	src := "```mermaid\ngraph LR; A-->B\n```\n\n```mermaid\ngraph LR; X\n```\n\n```mermaid\nno picture\n```\n"
	data, err := Convert([]byte(src), Options{Diagrams: map[string]Diagram{
		"graph LR; A-->B": {PNG: pngData.Bytes(), SVG: []byte("<svg/>"), Width: 100, Height: 50},
		"graph LR; X":     {PNG: nil, SVG: []byte("<svg/>")}, // no bitmap: stays code
	}})
	if err != nil {
		t.Fatal(err)
	}
	parts := unpack(t, data)
	if parts["word/media/image1.png"] == "" || parts["word/media/image1.svg"] != "<svg/>" {
		t.Error("diagram PNG and SVG not embedded")
	}
	doc := parts["word/document.xml"]
	if !strings.Contains(doc, `<w:pStyle w:val="Diagram"/>`) || !strings.Contains(doc, `<wp:extent cx="952500" cy="476250"/>`) || !strings.Contains(doc, "<asvg:svgBlip") {
		t.Errorf("diagram drawing: %s", doc)
	}
	if strings.Contains(doc, "A--&gt;B") || !strings.Contains(doc, "graph LR; X") || !strings.Contains(doc, "no picture") {
		t.Error("only diagrams with a bitmap should be replaced")
	}
	if !strings.Contains(parts["[Content_Types].xml"], `Extension="svg"`) {
		t.Error("svg content type missing")
	}
}
