package pxpipe

import (
	"bytes"
	"fmt"
	"image"
	"image/png"
	"strings"
	"testing"

	"github.com/maximhq/bifrost/core/schemas"
)

func structuredText(lines int) string {
	const row = `{"a":[1,2],"b":"x"}`
	rows := make([]string, lines)
	for i := range rows {
		rows[i] = row
	}
	return strings.Join(rows, "\n")
}

func logText(lines int) string {
	rows := make([]string, lines)
	for i := range rows {
		rows[i] = fmt.Sprintf("12:00:%02d INFO request %d handled by the worker in the background without any error", i%60, i)
	}
	return strings.Join(rows, "\n")
}

func decodePNG(t *testing.T, raw []byte) image.Image {
	t.Helper()
	img, err := png.Decode(bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("page does not decode as PNG: %v", err)
	}
	return img
}

func TestWidthShrink(t *testing.T) {
	g := newGeometry(180, 28)

	narrow := denseText(30, 20)
	eff, _ := g.plan(narrow)
	if !(eff.cols < 180) {
		t.Fatalf("eff.cols = %d, want < 180 for narrow content", eff.cols)
	}
	if eff.cols%g.colStep() != 0 {
		t.Fatalf("eff.cols = %d not aligned to colStep %d", eff.cols, g.colStep())
	}

	narrowTall := denseText(400, 20)
	s := buildSettings(Config{})
	if !profitable(narrowTall, s) {
		t.Fatalf("narrow-but-tall block should be profitable under width-shrink")
	}
	rows := len(layout(narrowTall, g.cols))
	shrunk := g.imageTokensFor(narrowTall)
	full := g.imageTokens(rows)
	if !(shrunk < full) {
		t.Fatalf("shrunk cost %d should be below full-width cost %d for %d rows", shrunk, full, rows)
	}
	if !(shrunk*2 < full) {
		t.Fatalf("width-shrink did not substantially cut cost: shrunk=%d full=%d", shrunk, full)
	}

	pngs, err := renderToPNGs(narrow, g)
	if err != nil {
		t.Fatalf("renderToPNGs: %v", err)
	}
	if len(pngs) == 0 {
		t.Fatalf("expected at least one page")
	}
	if w := decodePNG(t, pngs[0]).Bounds().Dx(); w >= 1260 {
		t.Fatalf("narrow page width = %d, want < 1260 (full-cols width)", w)
	}
}

func TestInkCycle(t *testing.T) {
	text := denseText(20, 100)
	plain := newGeometry(180, 28)
	inked := plain
	inked.inkCycle = true

	plainPngs, err := renderToPNGs(text, plain)
	if err != nil {
		t.Fatalf("renderToPNGs plain: %v", err)
	}
	inkedPngs, err := renderToPNGs(text, inked)
	if err != nil {
		t.Fatalf("renderToPNGs inked: %v", err)
	}
	if len(plainPngs) != len(inkedPngs) {
		t.Fatalf("page counts differ: plain %d, inked %d", len(plainPngs), len(inkedPngs))
	}

	for i := range plainPngs {
		pb := decodePNG(t, plainPngs[i]).Bounds()
		ib := decodePNG(t, inkedPngs[i]).Bounds()
		if pb != ib {
			t.Fatalf("page %d dims differ: plain %v, inked %v", i, pb, ib)
		}
	}
	if plain.imageTokensFor(text) != inked.imageTokensFor(text) {
		t.Fatalf("ink-cycle changed token cost: %d vs %d", plain.imageTokensFor(text), inked.imageTokensFor(text))
	}

	if bytes.Equal(plainPngs[0], inkedPngs[0]) {
		t.Fatalf("ink-cycle produced byte-identical output")
	}
	img := decodePNG(t, inkedPngs[0])
	b := img.Bounds()
	chromatic := false
	for y := b.Min.Y; y < b.Max.Y && !chromatic; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			r, gg, bl, _ := img.At(x, y).RGBA()
			if r != gg || gg != bl {
				chromatic = true
				break
			}
		}
	}
	if !chromatic {
		t.Fatalf("ink-cycle render has no chromatic pixel")
	}
}

func TestRepeatLines(t *testing.T) {
	text := denseText(80, 100)
	g1 := newGeometry(180, 28)
	g2 := newGeometry(180, 28)
	g2.repeat = 2

	base := layout(text, g1.cols)
	_, rows := g2.plan(text)
	if len(rows) != 2*len(base) {
		t.Fatalf("repeat=2 planned %d rows, want %d (2x %d)", len(rows), 2*len(base), len(base))
	}

	p1, err := renderToPNGs(text, g1)
	if err != nil {
		t.Fatalf("renderToPNGs g1: %v", err)
	}
	p2, err := renderToPNGs(text, g2)
	if err != nil {
		t.Fatalf("renderToPNGs g2: %v", err)
	}
	if !(len(p2) >= len(p1)) {
		t.Fatalf("repeat=2 page count %d < repeat=1 page count %d", len(p2), len(p1))
	}
	for i, raw := range p2 {
		if _, err := png.Decode(bytes.NewReader(raw)); err != nil {
			t.Fatalf("repeat page %d does not decode: %v", i, err)
		}
	}
	if t1, t2 := g1.imageTokensFor(text), g2.imageTokensFor(text); !(t2 > t1) {
		t.Fatalf("repeat=2 cost %d should exceed repeat=1 cost %d", t2, t1)
	}
}

func TestClassify(t *testing.T) {
	jsonText := structuredText(70)
	logs := logText(20)
	prose := denseText(50, 100)

	if got := classifyContent(jsonText); got != classStructured {
		t.Fatalf("classifyContent(json) = %d, want classStructured", got)
	}
	if got := classifyContent(logs); got != classLog {
		t.Fatalf("classifyContent(logs) = %d, want classLog", got)
	}
	if got := classifyContent(prose); got != classProse {
		t.Fatalf("classifyContent(prose) = %d, want classProse", got)
	}

	if got := gateCPT(jsonText, true, 4); got != structuredCharsPerToken {
		t.Fatalf("gateCPT(json, classify) = %v, want %v", got, structuredCharsPerToken)
	}
	if got := gateCPT(logs, true, 4); got != logCharsPerToken {
		t.Fatalf("gateCPT(logs, classify) = %v, want %v", got, logCharsPerToken)
	}
	if got := gateCPT(prose, true, 4); got != 4 {
		t.Fatalf("gateCPT(prose, classify) = %v, want 4", got)
	}
	if got := gateCPT(jsonText, false, 4); got != 4 {
		t.Fatalf("gateCPT(json, no classify) = %v, want 4 (base)", got)
	}

	sOff := settings{geom: newGeometry(180, 28), charsPerToken: 4, classify: false, outRatio: 5, decodeTax: 500, horizon: 8}
	sOn := sOff
	sOn.classify = true
	if profitable(jsonText, sOff) {
		t.Fatalf("structured block should not be profitable at the base density")
	}
	if !profitable(jsonText, sOn) {
		t.Fatalf("structured block should be profitable once classification lowers cpt")
	}
}

func TestCacheBreakpoint(t *testing.T) {
	big := denseText(300, 170)

	t.Run("marks last image of imaged prefix", func(t *testing.T) {
		p := enabledPlugin(t, Config{})
		req := newReq("claude-fable-5",
			toolStrMsg(big),
			toolStrMsg("t1"), toolStrMsg("t2"), toolStrMsg("t3"), toolStrMsg("t4"),
		)
		if err := p.PreRequestHook(nil, req); err != nil {
			t.Fatalf("PreRequestHook: %v", err)
		}
		blocks := req.ChatRequest.Input[0].Content.ContentBlocks
		if len(blocks) < 2 {
			t.Fatalf("want multiple image pages to test breakpoint placement, got %d", len(blocks))
		}
		last := blocks[len(blocks)-1]
		if last.CacheControl == nil || last.CacheControl.Type != schemas.CacheControlTypeEphemeral {
			t.Fatalf("last image block missing ephemeral cache breakpoint: %+v", last.CacheControl)
		}
		for i := range len(blocks) - 1 {
			if blocks[i].CacheControl != nil {
				t.Fatalf("non-last block %d unexpectedly carries a cache marker", i)
			}
		}
	})

	t.Run("disabled adds no marker", func(t *testing.T) {
		p := enabledPlugin(t, Config{CacheBreakpoint: new(false)})
		req := newReq("claude-fable-5",
			toolStrMsg(big),
			toolStrMsg("t1"), toolStrMsg("t2"), toolStrMsg("t3"), toolStrMsg("t4"),
		)
		if err := p.PreRequestHook(nil, req); err != nil {
			t.Fatalf("PreRequestHook: %v", err)
		}
		blocks := req.ChatRequest.Input[0].Content.ContentBlocks
		assertPNGImageBlocks(t, blocks)
		for i, blk := range blocks {
			if blk.CacheControl != nil {
				t.Fatalf("block %d got a cache marker despite CacheBreakpoint=false", i)
			}
		}
	})

	t.Run("respects the 4-marker limit", func(t *testing.T) {
		mkReq := func(existing int) *schemas.BifrostChatRequest {
			return &schemas.BifrostChatRequest{Input: []schemas.ChatMessage{
				{Content: &schemas.ChatMessageContent{ContentBlocks: []schemas.ChatContentBlock{imageBlock()}}},
				{Content: &schemas.ChatMessageContent{ContentBlocks: markedImageBlocks(existing)}},
			}}
		}

		atLimit := mkReq(maxCacheBreakpoints)
		if n := countCacheMarkers(atLimit); n != maxCacheBreakpoints {
			t.Fatalf("countCacheMarkers = %d, want %d", n, maxCacheBreakpoints)
		}
		placeCacheBreakpoint(atLimit, 0)
		if cc := atLimit.Input[0].Content.ContentBlocks[0].CacheControl; cc != nil {
			t.Fatalf("marker added despite being at the %d-marker limit", maxCacheBreakpoints)
		}

		below := mkReq(maxCacheBreakpoints - 1)
		if n := countCacheMarkers(below); n != maxCacheBreakpoints-1 {
			t.Fatalf("countCacheMarkers = %d, want %d", n, maxCacheBreakpoints-1)
		}
		placeCacheBreakpoint(below, 0)
		cc := below.Input[0].Content.ContentBlocks[0].CacheControl
		if cc == nil || cc.Type != schemas.CacheControlTypeEphemeral {
			t.Fatalf("no ephemeral marker added below the limit: %+v", cc)
		}
	})
}

func imageBlock() schemas.ChatContentBlock {
	return schemas.ChatContentBlock{
		Type:           schemas.ChatContentBlockTypeImage,
		ImageURLStruct: &schemas.ChatInputImage{URL: "data:image/png;base64,AAAA"},
	}
}

func markedImageBlocks(n int) []schemas.ChatContentBlock {
	b := make([]schemas.ChatContentBlock, n)
	for i := range b {
		b[i] = schemas.ChatContentBlock{
			Type:           schemas.ChatContentBlockTypeImage,
			ImageURLStruct: &schemas.ChatInputImage{URL: "x"},
			CacheControl:   &schemas.CacheControl{Type: schemas.CacheControlTypeEphemeral},
		}
	}
	return b
}

func TestRenderCache(t *testing.T) {
	t.Run("LRU eviction", func(t *testing.T) {
		c := newRenderCache(2)
		p1 := [][]byte{[]byte("a")}
		p2 := [][]byte{[]byte("b")}
		p3 := [][]byte{[]byte("c")}
		c.put("k1", p1)
		c.put("k2", p2)
		if _, ok := c.get("k1"); !ok {
			t.Fatalf("k1 should be present before eviction")
		}
		if _, ok := c.get("k2"); !ok {
			t.Fatalf("k2 should be present before eviction")
		}
		c.put("k3", p3)
		if _, ok := c.get("k1"); ok {
			t.Fatalf("k1 should have been evicted as the oldest entry")
		}
		if _, ok := c.get("k2"); !ok {
			t.Fatalf("k2 should survive eviction")
		}
		if _, ok := c.get("k3"); !ok {
			t.Fatalf("k3 should be present after insert")
		}
		if _, ok := c.get("absent"); ok {
			t.Fatalf("missing key should report not-found")
		}
	})

	t.Run("nil cache is disabled and safe", func(t *testing.T) {
		var c *renderCache = newRenderCache(0)
		if c != nil {
			t.Fatalf("newRenderCache(0) = %v, want nil", c)
		}
		if _, ok := c.get("x"); ok {
			t.Fatalf("nil cache get should report not-found")
		}
		c.put("x", [][]byte{[]byte("y")})
	})

	t.Run("key separates text and geometry", func(t *testing.T) {
		g := newGeometry(180, 28)
		g2 := newGeometry(180, 28)
		g2.repeat = 2
		if renderKey("aaa", g) == renderKey("bbb", g) {
			t.Fatalf("distinct text collided on the same key")
		}
		if renderKey("aaa", g) == renderKey("aaa", g2) {
			t.Fatalf("distinct geometry (repeat) collided on the same key")
		}
	})

	t.Run("render is deterministic", func(t *testing.T) {
		g := newGeometry(180, 28)
		text := denseText(50, 100)
		a, err := renderToPNGs(text, g)
		if err != nil {
			t.Fatalf("renderToPNGs a: %v", err)
		}
		b, err := renderToPNGs(text, g)
		if err != nil {
			t.Fatalf("renderToPNGs b: %v", err)
		}
		if len(a) != len(b) {
			t.Fatalf("page counts differ across renders: %d vs %d", len(a), len(b))
		}
		for i := range a {
			if !bytes.Equal(a[i], b[i]) {
				t.Fatalf("page %d differs across identical renders", i)
			}
		}
	})
}

func TestModelProfiles(t *testing.T) {
	p, err := Init(Config{
		Enabled: true,
		Models:  []string{"claude-fable-5"},
		ModelProfiles: map[string]ProfileConfig{
			"opus": {OutputInputPriceRatio: new(15.0), PatchPx: new(14)},
		},
	}, nil)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	opus := p.settingsFor("claude-opus-4-8")
	if opus.outRatio != 15 {
		t.Fatalf("opus outRatio = %v, want 15", opus.outRatio)
	}
	if opus.geom.patchPx != 14 {
		t.Fatalf("opus patchPx = %d, want 14", opus.geom.patchPx)
	}

	fable := p.settingsFor("claude-fable-5")
	if fable.outRatio != 5 {
		t.Fatalf("fable (base) outRatio = %v, want 5", fable.outRatio)
	}
	if fable.geom.patchPx != 28 {
		t.Fatalf("fable (base) patchPx = %d, want 28", fable.geom.patchPx)
	}

	if !p.modelAllowed("claude-opus-4-8") {
		t.Fatalf("profile key should extend the allowlist")
	}
	if p.modelAllowed("gpt-3.5") {
		t.Fatalf("unrelated model should not be allowed")
	}

	p2, err := Init(Config{
		Enabled: true,
		Models:  []string{"claude-fable-5"},
		ModelProfiles: map[string]ProfileConfig{
			"claude":      {OutputInputPriceRatio: new(9.0)},
			"claude-opus": {OutputInputPriceRatio: new(21.0)},
		},
	}, nil)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}
	if got := p2.settingsFor("claude-opus-4-8").outRatio; got != 21 {
		t.Fatalf("most-specific profile outRatio = %v, want 21 (longer 'claude-opus' key)", got)
	}
}
