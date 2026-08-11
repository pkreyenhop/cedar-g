package main

import (
	"fmt"
	"html"
	"os"
	"strings"

	"cedarg/internal/tioga"
)

// exportHTML renders decoded blocks as a self-contained HTML document — the
// "what is printed" view: headings by format, looks as inline tags, tab tables
// as real tables, and text colour preserved. A browser can print it to PDF.
func exportHTML(title string, blocks []tioga.Block) string {
	var b strings.Builder
	b.WriteString("<!doctype html>\n<html><head><meta charset=\"utf-8\">\n")
	fmt.Fprintf(&b, "<title>%s</title>\n", html.EscapeString(title))
	b.WriteString(`<style>
body{font-family:Georgia,'Times New Roman',serif;max-width:44rem;margin:2rem auto;padding:0 1rem;line-height:1.4;color:#111}
h1,h2,h3,h4,h5{font-family:inherit}
.title{text-align:center}
.center{text-align:center}
pre{font-family:Menlo,Consolas,monospace;white-space:pre-wrap;background:#f6f6f6;padding:.4rem .6rem;border-radius:3px}
table{border-collapse:collapse;margin:.4rem 0}
td{padding:.05rem .6rem}
td.num{text-align:right;font-variant-numeric:tabular-nums}
.sc{font-variant:small-caps}
.fig{color:#666;font-style:italic;border:1px solid #ccc;padding:.4rem;text-align:center}
</style>
</head><body>
`)
	for _, blk := range blocks {
		b.WriteString(blockHTML(blk))
		b.WriteByte('\n')
	}
	b.WriteString("</body></html>\n")
	return b.String()
}

// blockHTML renders one block.
func blockHTML(b tioga.Block) string {
	if len(b.Art) > 0 {
		return `<p class="fig">[Figure]</p>`
	}
	if looksLikeTable(b) {
		return tableHTML(b)
	}
	inner := runsHTML(runsOrText(b))
	if c := colorStyle(b); c != "" {
		inner = `<span style="` + c + `">` + inner + `</span>`
	}
	switch {
	case isHeadFormat(b.Format):
		lvl := headLevel(b.Format, b.Depth)
		if lvl > 5 {
			lvl = 5
		}
		return fmt.Sprintf("<h%d>%s</h%d>", lvl, inner, lvl)
	case b.Format == "title":
		return `<h1 class="title">` + inner + `</h1>`
	case b.Format == "subtitle", b.Format == "authors", b.Format == "center":
		return `<p class="center">` + inner + `</p>`
	case isCodeFormat(b.Format):
		return "<pre>" + inner + "</pre>"
	default:
		return "<p>" + inner + "</p>"
	}
}

// runsOrText returns a block's runs, synthesising one plain run from Text.
func runsOrText(b tioga.Block) []tioga.Run {
	if len(b.Runs) > 0 {
		return b.Runs
	}
	return []tioga.Run{{Text: b.Text}}
}

// runsHTML renders styled runs as inline HTML, wrapping each run in the tags its
// looks call for.
func runsHTML(runs []tioga.Run) string {
	var b strings.Builder
	for _, r := range runs {
		txt := html.EscapeString(r.Text)
		lk := r.Look
		var pre, post string
		open := func(o, c string) { pre += o; post = c + post }
		if lk.Bold() {
			open("<b>", "</b>")
		}
		if lk.Italic() {
			open("<i>", "</i>")
		}
		if lk.Underline() {
			open("<u>", "</u>")
		}
		if lk.Has('x') {
			open("<s>", "</s>")
		}
		if lk.Has('k') || lk.Has('p') {
			open("<code>", "</code>")
		}
		if lk.Has('h') {
			open("<sup>", "</sup>")
		}
		if lk.Has('l') {
			open("<sub>", "</sub>")
		}
		if lk.Has('e') {
			open(`<span class="sc">`, "</span>")
		}
		b.WriteString(pre)
		b.WriteString(txt)
		b.WriteString(post)
	}
	return b.String()
}

// tableHTML renders a table block as an HTML table using the same grid split.
func tableHTML(b tioga.Block) string {
	grid := tableGrid(runsOrText(b))
	ncol := 0
	for _, row := range grid {
		if len(row) > ncol {
			ncol = len(row)
		}
	}
	numeric := make([]bool, ncol)
	tot := make([]int, ncol)
	hit := make([]int, ncol)
	for _, row := range grid {
		for c, cell := range row {
			if !cell.empty() {
				tot[c]++
				if isNumericText(cell.text()) {
					hit[c]++
				}
			}
		}
	}
	for c := 0; c < ncol; c++ {
		numeric[c] = c > 0 && tot[c] > 0 && hit[c]*2 >= tot[c]
	}
	var b2 strings.Builder
	b2.WriteString("<table>")
	for _, row := range grid {
		b2.WriteString("<tr>")
		for c := 0; c < ncol; c++ {
			cls := ""
			if c < ncol && numeric[c] {
				cls = ` class="num"`
			}
			cell := ""
			if c < len(row) {
				cell = runsHTML(row[c].runs)
			}
			b2.WriteString("<td" + cls + ">" + cell + "</td>")
		}
		b2.WriteString("</tr>")
	}
	b2.WriteString("</table>")
	return b2.String()
}

// colorStyle returns a CSS colour declaration for a block's text colour.
func colorStyle(b tioga.Block) string {
	if b.Fg == nil {
		return ""
	}
	cl := func(f float32) int {
		if f < 0 {
			f = 0
		}
		if f > 1 {
			f = 1
		}
		return int(f*255 + 0.5)
	}
	return fmt.Sprintf("color:#%02x%02x%02x", cl(b.Fg.R), cl(b.Fg.G), cl(b.Fg.B))
}

// exportDoc writes a viewer's document to an HTML file beside its source.
func (s *gioUI) exportDoc(v *viewer) {
	if !v.isDoc() {
		return
	}
	path := v.path + ".html"
	if err := os.WriteFile(path, []byte(exportHTML(v.headerTitle(), v.blocks)), 0o644); err != nil {
		v.saveMsg = "export failed: " + err.Error()
		s.setMessage(v.saveMsg)
		return
	}
	v.saveMsg = "exported " + s.relPath(path)
	s.setMessage(v.saveMsg)
}
