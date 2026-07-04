package pxpipe

import (
	"bytes"
	"encoding/base64"
	"image/png"
	"strings"
	"testing"

	"github.com/maximhq/bifrost/core/schemas"
)

const dataURIPrefix = "data:image/png;base64,"

func enabledPlugin(t *testing.T, cfg Config) *PxpipePlugin {
	t.Helper()
	cfg.Enabled = true
	p, err := Init(cfg, nil)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}
	return p
}

func toolStrMsg(s string) schemas.ChatMessage {
	return schemas.ChatMessage{
		Role:    schemas.ChatMessageRoleTool,
		Content: &schemas.ChatMessageContent{ContentStr: new(s)},
	}
}

func newReq(model string, msgs ...schemas.ChatMessage) *schemas.BifrostRequest {
	return &schemas.BifrostRequest{
		ChatRequest: &schemas.BifrostChatRequest{Model: model, Input: msgs},
	}
}

func assertPNGImageBlocks(t *testing.T, blocks []schemas.ChatContentBlock) {
	t.Helper()
	if len(blocks) == 0 {
		t.Fatalf("expected image blocks, got none")
	}
	for i, blk := range blocks {
		if blk.Type != schemas.ChatContentBlockTypeImage {
			t.Fatalf("block %d type = %q, want %q", i, blk.Type, schemas.ChatContentBlockTypeImage)
		}
		if blk.ImageURLStruct == nil {
			t.Fatalf("block %d has nil ImageURLStruct", i)
		}
		url := blk.ImageURLStruct.URL
		payload, ok := strings.CutPrefix(url, dataURIPrefix)
		if !ok {
			t.Fatalf("block %d url %q lacks prefix %q", i, url, dataURIPrefix)
		}
		raw, err := base64.StdEncoding.DecodeString(payload)
		if err != nil {
			t.Fatalf("block %d base64 decode: %v", i, err)
		}
		if _, err := png.Decode(bytes.NewReader(raw)); err != nil {
			t.Fatalf("block %d payload is not a PNG: %v", i, err)
		}
	}
}

func TestPreRequestHook_Disabled(t *testing.T) {
	p, err := Init(Config{Enabled: false}, nil)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}
	req := newReq("claude-fable-5", toolStrMsg(denseText(300, 170)))
	orig := req.ChatRequest.Input[0].Content.ContentStr

	if err := p.PreRequestHook(nil, req); err != nil {
		t.Fatalf("PreRequestHook: %v", err)
	}

	c := req.ChatRequest.Input[0].Content
	if c.ContentStr != orig {
		t.Fatalf("disabled plugin mutated ContentStr pointer")
	}
	if c.ContentBlocks != nil {
		t.Fatalf("disabled plugin added content blocks")
	}
}

func TestPreRequestHook_ModelNotAllowed(t *testing.T) {
	p := enabledPlugin(t, Config{})
	req := newReq("gpt-3.5", toolStrMsg(denseText(300, 170)))
	orig := req.ChatRequest.Input[0].Content.ContentStr

	if err := p.PreRequestHook(nil, req); err != nil {
		t.Fatalf("PreRequestHook: %v", err)
	}

	c := req.ChatRequest.Input[0].Content
	if c.ContentStr != orig || c.ContentBlocks != nil {
		t.Fatalf("non-allowlisted model was transformed")
	}
}

func TestPreRequestHook_ImagesOldEligibleMessage(t *testing.T) {
	p := enabledPlugin(t, Config{})
	req := newReq("claude-fable-5",
		toolStrMsg(denseText(300, 170)),
		toolStrMsg("s1"), toolStrMsg("s2"), toolStrMsg("s3"), toolStrMsg("s4"),
	)

	if err := p.PreRequestHook(nil, req); err != nil {
		t.Fatalf("PreRequestHook: %v", err)
	}

	c := req.ChatRequest.Input[0].Content
	if c.ContentStr != nil {
		t.Fatalf("eligible old message left as text")
	}
	assertPNGImageBlocks(t, c.ContentBlocks)
}

func TestPreRequestHook_PreservesLiveTail(t *testing.T) {
	big := denseText(300, 170)

	req := newReq("claude-fable-5",
		toolStrMsg(big),
		toolStrMsg("small"),
		toolStrMsg(big),
		toolStrMsg(big),
		toolStrMsg(big),
		toolStrMsg(big),
	)
	p := enabledPlugin(t, Config{})

	tailPtrs := make([]*string, 0, 4)
	for i := 2; i <= 5; i++ {
		tailPtrs = append(tailPtrs, req.ChatRequest.Input[i].Content.ContentStr)
	}

	if err := p.PreRequestHook(nil, req); err != nil {
		t.Fatalf("PreRequestHook: %v", err)
	}

	in := req.ChatRequest.Input
	if in[0].Content.ContentStr != nil {
		t.Fatalf("eligible message before tail was not imaged")
	}
	assertPNGImageBlocks(t, in[0].Content.ContentBlocks)

	for offset, i := range []int{2, 3, 4, 5} {
		c := in[i].Content
		if c.ContentStr != tailPtrs[offset] {
			t.Fatalf("tail message %d text was mutated", i)
		}
		if c.ContentBlocks != nil {
			t.Fatalf("tail message %d was imaged despite live tail", i)
		}
	}
}

func TestPreRequestHook_SmallMessageKeptText(t *testing.T) {
	p := enabledPlugin(t, Config{})
	req := newReq("claude-fable-5",
		toolStrMsg(denseText(3, 30)),
		toolStrMsg("t1"), toolStrMsg("t2"), toolStrMsg("t3"), toolStrMsg("t4"),
	)
	orig := req.ChatRequest.Input[0].Content.ContentStr

	if err := p.PreRequestHook(nil, req); err != nil {
		t.Fatalf("PreRequestHook: %v", err)
	}

	c := req.ChatRequest.Input[0].Content
	if c.ContentStr != orig || c.ContentBlocks != nil {
		t.Fatalf("sub-MinChars message was imaged")
	}
}

func TestPreRequestHook_CacheControlOnlyOnLastImage(t *testing.T) {
	big := denseText(300, 170)
	cache := &schemas.CacheControl{}
	msg := schemas.ChatMessage{
		Role: schemas.ChatMessageRoleTool,
		Content: &schemas.ChatMessageContent{
			ContentBlocks: []schemas.ChatContentBlock{
				{Type: schemas.ChatContentBlockTypeText, Text: new(big), CacheControl: cache},
			},
		},
	}
	req := newReq("claude-fable-5", msg,
		toolStrMsg("t1"), toolStrMsg("t2"), toolStrMsg("t3"), toolStrMsg("t4"),
	)
	p := enabledPlugin(t, Config{})

	if err := p.PreRequestHook(nil, req); err != nil {
		t.Fatalf("PreRequestHook: %v", err)
	}

	blocks := req.ChatRequest.Input[0].Content.ContentBlocks
	if len(blocks) < 2 {
		t.Fatalf("want multiple image pages to test cache placement, got %d", len(blocks))
	}
	assertPNGImageBlocks(t, blocks)

	last := len(blocks) - 1
	for i, blk := range blocks {
		if i == last {
			if blk.CacheControl != cache {
				t.Fatalf("last image block lost the cache_control marker")
			}
		} else if blk.CacheControl != nil {
			t.Fatalf("non-last image block %d carries a cache_control marker", i)
		}
	}
}

func TestPreRequestHook_NonChatRequestNoPanic(t *testing.T) {
	p := enabledPlugin(t, Config{})
	cases := map[string]*schemas.BifrostRequest{
		"nil request":      nil,
		"nil chat request": {},
	}
	for name, req := range cases {
		t.Run(name, func(t *testing.T) {
			if err := p.PreRequestHook(nil, req); err != nil {
				t.Fatalf("PreRequestHook: %v", err)
			}
		})
	}
}

func TestInit_ClampsColsToNoDownscaleBound(t *testing.T) {
	p, err := Init(Config{Enabled: true, Cols: 10000}, nil)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}
	if w := p.base.geom.pageWidthPx(); w > maxWidthPx {
		t.Fatalf("oversized Cols not clamped: page width %d exceeds no-downscale bound %d", w, maxWidthPx)
	}
}

func TestInit_HonorsCustomMinChars(t *testing.T) {
	big := denseText(300, 170)
	p := enabledPlugin(t, Config{MinChars: 100000})
	req := newReq("claude-fable-5",
		toolStrMsg(big),
		toolStrMsg("t1"), toolStrMsg("t2"), toolStrMsg("t3"), toolStrMsg("t4"),
	)
	orig := req.ChatRequest.Input[0].Content.ContentStr

	if err := p.PreRequestHook(nil, req); err != nil {
		t.Fatalf("PreRequestHook: %v", err)
	}

	c := req.ChatRequest.Input[0].Content
	if c.ContentStr != orig || c.ContentBlocks != nil {
		t.Fatalf("custom MinChars ignored: block below threshold was imaged")
	}
}

func TestModelAllowed(t *testing.T) {
	p := enabledPlugin(t, Config{})
	tests := []struct {
		name  string
		model string
		want  bool
	}{
		{"exact match", "claude-fable-5", true},
		{"case-insensitive", "CLAUDE-FABLE-5", true},
		{"substring inside longer id", "anthropic/claude-fable-5-preview", true},
		{"second allowlist entry", "openai/GPT-5.6-turbo", true},
		{"empty model", "", false},
		{"unrelated model", "gpt-3.5-turbo", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := p.modelAllowed(tc.model); got != tc.want {
				t.Fatalf("modelAllowed(%q) = %v, want %v", tc.model, got, tc.want)
			}
		})
	}
}
