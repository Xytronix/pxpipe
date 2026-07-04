package pxpipe

import (
	"bytes"
	"image/png"
	"reflect"
	"strings"
	"testing"
)

func TestLayout(t *testing.T) {
	tests := []struct {
		name string
		text string
		want []string
	}{
		{

			name: "wrap long line into ceil rows",
			text: strings.Repeat("x", 400),
			want: []string{strings.Repeat("x", 180), strings.Repeat("x", 180), strings.Repeat("x", 40)},
		},
		{
			name: "short line stays one row",
			text: "hello",
			want: []string{"hello"},
		},
		{

			name: "collapse run of five blanks to two",
			text: "a" + strings.Repeat("\n", 6) + "b",
			want: []string{"a", "", "", "b"},
		},
		{
			name: "leading tab expands to spaces",
			text: "\tx",
			want: []string{"    x"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := layout(tc.text, defaultCols)
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("layout(%q) = %#v, want %#v", tc.text, got, tc.want)
			}
			for i, row := range got {
				if n := len([]rune(row)); n > defaultCols {
					t.Fatalf("row %d has %d runes, exceeds cols=%d", i, n, defaultCols)
				}
			}
		})
	}
}

func TestRenderToPNGs(t *testing.T) {
	geoms := []struct {
		name string
		g    geometry
	}{
		{"aligned", newGeometry(defaultCols, defaultPatchPx)},
		{"unaligned", newGeometry(defaultCols, 0)},
	}
	for _, gc := range geoms {
		g := gc.g
		rpp := g.rowsPerPage

		tests := []struct {
			name      string
			text      string
			wantPages int
		}{
			{"single partial page", denseText(10, 100), 1},
			{"exactly one full page", denseText(rpp, 120), 1},
			{"just over one page spills to two", denseText(rpp+1, 120), 2},
			{"three full pages plus a partial", denseText(3*rpp+5, 170), 4},
		}
		for _, tc := range tests {
			t.Run(gc.name+"/"+tc.name, func(t *testing.T) {
				pngs, err := renderToPNGs(tc.text, g)
				if err != nil {
					t.Fatalf("renderToPNGs: %v", err)
				}
				if len(pngs) != tc.wantPages {
					t.Fatalf("got %d pages, want %d", len(pngs), tc.wantPages)
				}

				eff, rows := g.plan(tc.text)
				for i, raw := range pngs {
					img, err := png.Decode(bytes.NewReader(raw))
					if err != nil {
						t.Fatalf("page %d does not decode as PNG: %v", i, err)
					}
					b := img.Bounds()

					rowsInPage := len(rows) - i*eff.rowsPerPage
					if rowsInPage > eff.rowsPerPage {
						rowsInPage = eff.rowsPerPage
					}
					wantWidth := eff.pageWidthPx()
					wantHeight := eff.pageHeightPx(rowsInPage)

					if b.Dx() != wantWidth {
						t.Fatalf("page %d width = %d, want %d", i, b.Dx(), wantWidth)
					}
					if b.Dy() != wantHeight {
						t.Fatalf("page %d height = %d, want %d (%d rows)", i, b.Dy(), wantHeight, rowsInPage)
					}
					if b.Dy() > maxHeightPx {
						t.Fatalf("page %d height %d exceeds maxHeightPx %d", i, b.Dy(), maxHeightPx)
					}
					if eff.patchPx > 0 {
						if b.Dx()%eff.patchPx != 0 {
							t.Fatalf("page %d width %d is not a whole patch (%d)", i, b.Dx(), eff.patchPx)
						}
						if b.Dy()%eff.patchPx != 0 {
							t.Fatalf("page %d height %d is not a whole patch (%d)", i, b.Dy(), eff.patchPx)
						}
					}
				}
			})
		}
	}
}
