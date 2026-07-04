package pxpipe

import (
	"bytes"
	"image/png"
	"math"
	"strings"
	"testing"
)

func denseText(lines, width int) string {
	const alphabet = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	var b strings.Builder
	b.Grow(lines * (width + 1))
	for i := range lines {
		if i > 0 {
			b.WriteByte('\n')
		}
		for j := range width {
			b.WriteByte(alphabet[(i*7+j)%len(alphabet)])
		}
	}
	return b.String()
}

func TestImageTokens(t *testing.T) {
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
		t.Run(gc.name, func(t *testing.T) {
			t.Run("non-positive rows cost nothing", func(t *testing.T) {
				for _, rows := range []int{0, -1, -rpp} {
					if got := g.imageTokens(rows); got != 0 {
						t.Fatalf("imageTokens(%d) = %d, want 0", rows, got)
					}
				}
			})

			t.Run("one row costs something", func(t *testing.T) {
				if got := g.imageTokens(1); got <= 0 {
					t.Fatalf("imageTokens(1) = %d, want > 0", got)
				}
			})

			t.Run("non-decreasing across the page boundary", func(t *testing.T) {
				prev := g.imageTokens(1)
				for rows := 2; rows <= 3*rpp+5; rows++ {
					got := g.imageTokens(rows)
					if got < prev {
						t.Fatalf("cost dropped at rows=%d: %d < %d (prev)", rows, got, prev)
					}
					prev = got
				}
			})

			t.Run("one full page cheaper than two", func(t *testing.T) {
				one := g.imageTokens(rpp)
				two := g.imageTokens(2 * rpp)
				if !(one < two) {
					t.Fatalf("one page (%d) should cost less than two pages (%d)", one, two)
				}
			})
		})
	}
}

func TestImageTokens_MatchesRenderedPixels(t *testing.T) {
	text := denseText(300, 170)
	geoms := []struct {
		name string
		g    geometry
	}{
		{"aligned", newGeometry(defaultCols, defaultPatchPx)},
		{"unaligned", newGeometry(defaultCols, 0)},
	}
	for _, gc := range geoms {
		g := gc.g
		t.Run(gc.name, func(t *testing.T) {
			pngs, err := renderToPNGs(text, g)
			if err != nil {
				t.Fatalf("renderToPNGs: %v", err)
			}
			if len(pngs) < 2 {
				t.Fatalf("want a multi-page render to exercise the invariant, got %d page(s)", len(pngs))
			}

			var totalPixels float64
			for i, raw := range pngs {
				img, err := png.Decode(bytes.NewReader(raw))
				if err != nil {
					t.Fatalf("page %d decode: %v", i, err)
				}
				b := img.Bounds()
				totalPixels += float64(b.Dx() * b.Dy())
			}
			want := int(math.Ceil(totalPixels / pixelsPerToken * imageCostSafetyMargin))

			if got := g.imageTokensFor(text); got != want {
				t.Fatalf("imageTokensFor = %d, but rendered pixels imply %d", got, want)
			}
		})
	}
}

func TestProfitable(t *testing.T) {

	s := settings{geom: newGeometry(defaultCols, defaultPatchPx), charsPerToken: 4, classify: false, outRatio: 5, decodeTax: 500, horizon: 8}
	tests := []struct {
		name string
		text string
		want bool
	}{
		{"large dense block wins", denseText(300, 170), true},
		{"empty never profitable", "", false},
		{"short block loses", denseText(5, 30), false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := profitable(tc.text, s); got != tc.want {
				t.Fatalf("profitable(%d chars) = %v, want %v", len([]rune(tc.text)), got, tc.want)
			}
		})
	}
}

func TestProfitable_DecodeTaxFlip(t *testing.T) {

	text := denseText(15, 167)

	withTax := settings{geom: newGeometry(defaultCols, defaultPatchPx), charsPerToken: 4, classify: false, outRatio: 5, decodeTax: 500, horizon: 8}
	if profitable(text, withTax) {
		t.Fatalf("borderline block should fail the gate under the decode tax")
	}

	noTax := withTax
	noTax.decodeTax = 0
	if !profitable(text, noTax) {
		t.Fatalf("same block should pass once the decode tax is disabled (it still saves input)")
	}
}

func TestProfitable_HorizonMonotonic(t *testing.T) {
	text := denseText(15, 167)
	base := settings{geom: newGeometry(defaultCols, defaultPatchPx), charsPerToken: 4, classify: false, outRatio: 5, decodeTax: 500}

	horizons := []int{1, 2, 4, 8, 16, 32, 64, 128}
	sawTrue := false
	for _, h := range horizons {
		s := base
		s.horizon = h
		got := profitable(text, s)
		if got {
			sawTrue = true
		} else if sawTrue {
			t.Fatalf("profitable regressed to false at Horizon=%d after being true", h)
		}
	}

	s := base
	s.horizon = horizons[0]
	if profitable(text, s) {
		t.Fatalf("expected false at Horizon=%d (full decode tax)", s.horizon)
	}
	s.horizon = horizons[len(horizons)-1]
	if !profitable(text, s) {
		t.Fatalf("expected true at Horizon=%d (amortized decode tax)", s.horizon)
	}
}
