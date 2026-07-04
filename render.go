package pxpipe

import (
	"bytes"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"strings"
	"unicode/utf8"

	"golang.org/x/image/font"
	"golang.org/x/image/font/basicfont"
	"golang.org/x/image/math/fixed"
)

const (
	cellW = 7

	glyphH = 13
	ascent = 11

	maxWidthPx = 1568

	maxHeightPx = 1560

	defaultCols = 180

	defaultPatchPx = 28

	tabWidth = 4

	unalignedPad = 4
)

var blackUniform = image.NewUniform(color.Black)

var inkUniforms = func() []*image.Uniform {
	hues := []color.RGBA{
		{0x00, 0x00, 0x00, 0xff},
		{0x1a, 0x1a, 0x9e, 0xff},
		{0x0e, 0x6b, 0x0e, 0xff},
		{0x9e, 0x1a, 0x1a, 0xff},
		{0x6b, 0x1a, 0x8e, 0xff},
		{0x7a, 0x5a, 0x00, 0xff},
	}
	u := make([]*image.Uniform, len(hues))
	for i, h := range hues {
		u[i] = image.NewUniform(h)
	}
	return u
}()

type geometry struct {
	cols        int
	cellW       int
	cellH       int
	padX        int
	padY        int
	patchPx     int
	rowsPerPage int
	repeat      int
	inkCycle    bool
}

func newGeometry(cols, patchPx int) geometry {
	if cols < 1 {
		cols = 1
	}
	g := geometry{cols: cols, cellW: cellW, repeat: 1}
	if patchPx >= glyphH {
		cellH := smallestDivisorAtLeast(patchPx, glyphH)
		if cellH == 0 {
			cellH = patchPx
		}
		g.patchPx = patchPx
		g.cellH = cellH
		colStep := patchPx / gcd(patchPx, cellW)
		g.cols = (cols / colStep) * colStep
		if g.cols < colStep {
			g.cols = colStep
		}
		rowStep := patchPx / gcd(patchPx, cellH)
		g.rowsPerPage = (maxHeightPx / cellH / rowStep) * rowStep
		if g.rowsPerPage < rowStep {
			g.rowsPerPage = rowStep
		}
	} else {
		g.cellH = glyphH
		g.padX, g.padY = unalignedPad, unalignedPad
		g.rowsPerPage = (maxHeightPx - 2*g.padY) / g.cellH
	}
	if g.rowsPerPage < 1 {
		g.rowsPerPage = 1
	}
	return g
}

func (g geometry) pageWidthPx() int {
	w := 2*g.padX + g.cols*g.cellW
	if g.patchPx > 0 {
		w = alignUp(w, g.patchPx)
	}
	return w
}

func (g geometry) pageHeightPx(rows int) int {
	h := 2*g.padY + rows*g.cellH
	if g.patchPx > 0 {
		h = alignUp(h, g.patchPx)
	}
	return h
}

func (g geometry) colStep() int {
	if g.patchPx <= 0 {
		return 1
	}
	return g.patchPx / gcd(g.patchPx, g.cellW)
}

func (g geometry) plan(text string) (geometry, []string) {
	rows := layout(text, g.cols)
	eff := g
	if w := widestCols(rows); w < g.cols {
		if g.patchPx > 0 {
			w = min(alignUp(w, g.colStep()), g.cols)
		}
		if w < 1 {
			w = 1
		}
		eff.cols = w
	}
	if g.repeat > 1 {
		rep := make([]string, 0, len(rows)*g.repeat)
		for _, r := range rows {
			for range g.repeat {
				rep = append(rep, r)
			}
		}
		rows = rep
	}
	return eff, rows
}

func gcd(a, b int) int {
	for b != 0 {
		a, b = b, a%b
	}
	if a < 0 {
		return -a
	}
	return a
}

func alignUp(x, m int) int {
	if m <= 0 {
		return x
	}
	return ((x + m - 1) / m) * m
}

func smallestDivisorAtLeast(n, min int) int {
	for d := min; d <= n; d++ {
		if n%d == 0 {
			return d
		}
	}
	return 0
}

func widestCols(rows []string) int {
	w := 1
	for _, r := range rows {
		if n := utf8.RuneCountInString(r); n > w {
			w = n
		}
	}
	return w
}

func expandTabs(line string) string {
	if !strings.ContainsRune(line, '\t') {
		return line
	}
	var b strings.Builder
	b.Grow(len(line))
	col := 0
	for _, r := range line {
		if r == '\t' {
			n := tabWidth - (col % tabWidth)
			for range n {
				b.WriteByte(' ')
			}
			col += n
			continue
		}
		b.WriteRune(r)
		col++
	}
	return b.String()
}

func layout(text string, cols int) []string {
	if cols < 1 {
		cols = 1
	}
	var rows []string
	blanks := 0
	for _, raw := range strings.Split(text, "\n") {
		line := expandTabs(strings.TrimRight(raw, " \t"))
		if line == "" {
			blanks++
			if blanks <= 2 {
				rows = append(rows, "")
			}
			continue
		}
		blanks = 0
		runes := []rune(line)
		for len(runes) > cols {
			rows = append(rows, string(runes[:cols]))
			runes = runes[cols:]
		}
		rows = append(rows, string(runes))
	}
	return rows
}

func renderToPNGs(text string, g geometry) ([][]byte, error) {
	eff, rows := g.plan(text)
	if len(rows) == 0 {
		return nil, nil
	}
	var pages [][]byte
	for start := 0; start < len(rows); start += eff.rowsPerPage {
		end := min(start+eff.rowsPerPage, len(rows))
		p, err := renderPage(rows[start:end], eff)
		if err != nil {
			return nil, err
		}
		pages = append(pages, p)
	}
	return pages, nil
}

func renderPage(rows []string, g geometry) ([]byte, error) {
	img := image.NewRGBA(image.Rect(0, 0, g.pageWidthPx(), g.pageHeightPx(len(rows))))
	draw.Draw(img, img.Bounds(), image.NewUniform(color.White), image.Point{}, draw.Src)

	d := &font.Drawer{Dst: img, Face: basicfont.Face7x13}
	for i, line := range rows {
		d.Src = inkFor(g, i)
		d.Dot = fixed.P(g.padX, g.padY+i*g.cellH+ascent)
		d.DrawString(line)
	}

	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func inkFor(g geometry, row int) image.Image {
	if !g.inkCycle {
		return blackUniform
	}
	return inkUniforms[row%len(inkUniforms)]
}
