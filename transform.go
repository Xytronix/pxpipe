package pxpipe

import (
	"encoding/base64"
	"unicode/utf8"

	"github.com/maximhq/bifrost/core/schemas"
)

const maxCacheBreakpoints = 4

type transformStats struct {
	blocksImaged    int
	imagesProduced  int
	textCharsImaged int
	textTokensEst   int
	imageTokensEst  int
}

func (s transformStats) tokensSaved() int { return s.textTokensEst - s.imageTokensEst }
func (s transformStats) touched() bool    { return s.blocksImaged > 0 }

func (p *PxpipePlugin) transformChat(cr *schemas.BifrostChatRequest, s settings) transformStats {
	var stats transformStats
	n := len(cr.Input)
	tailStart := n - p.config.KeepRecentMessages
	lastImaged := -1
	for i := range cr.Input {
		if i >= tailStart {
			break
		}
		if stats.imagesProduced >= p.config.MaxImages {
			break
		}
		if p.transformMessage(&cr.Input[i], s, &stats) {
			lastImaged = i
		}
	}
	if lastImaged >= 0 && s.cacheBreak {
		placeCacheBreakpoint(cr, lastImaged)
	}
	return stats
}

func (p *PxpipePlugin) transformMessage(m *schemas.ChatMessage, s settings, stats *transformStats) bool {
	c := m.Content
	if c == nil {
		return false
	}

	if c.ContentStr != nil {
		text := *c.ContentStr
		if !p.eligible(text, s) {
			return false
		}
		blocks := p.imageBlocks(text, nil, s, stats)
		if blocks == nil {
			return false
		}
		c.ContentStr = nil
		c.ContentBlocks = blocks
		return true
	}

	if c.ContentBlocks == nil {
		return false
	}
	out := make([]schemas.ChatContentBlock, 0, len(c.ContentBlocks))
	changed := false
	for _, blk := range c.ContentBlocks {
		if blk.Type == schemas.ChatContentBlockTypeText && blk.Text != nil && p.eligible(*blk.Text, s) {
			if imgs := p.imageBlocks(*blk.Text, blk.CacheControl, s, stats); imgs != nil {
				out = append(out, imgs...)
				changed = true
				continue
			}
		}
		out = append(out, blk)
	}
	if changed {
		c.ContentBlocks = out
	}
	return changed
}

func (p *PxpipePlugin) eligible(text string, s settings) bool {
	if utf8.RuneCountInString(text) < s.minChars {
		return false
	}
	return profitable(text, s)
}

func (p *PxpipePlugin) imageBlocks(text string, cache *schemas.CacheControl, s settings, stats *transformStats) []schemas.ChatContentBlock {
	remaining := p.config.MaxImages - stats.imagesProduced
	if remaining <= 0 {
		return nil
	}
	pngs := p.renderCached(text, s.geom)
	if len(pngs) == 0 || len(pngs) > remaining {
		return nil
	}

	blocks := make([]schemas.ChatContentBlock, 0, len(pngs))
	for _, raw := range pngs {
		url := "data:image/png;base64," + base64.StdEncoding.EncodeToString(raw)
		blocks = append(blocks, schemas.ChatContentBlock{
			Type:           schemas.ChatContentBlockTypeImage,
			ImageURLStruct: &schemas.ChatInputImage{URL: url},
		})
	}
	if cache != nil {
		blocks[len(blocks)-1].CacheControl = cache
	}

	chars := utf8.RuneCountInString(text)
	stats.blocksImaged++
	stats.imagesProduced += len(pngs)
	stats.textCharsImaged += chars
	stats.textTokensEst += int(float64(chars) / reportCharsPerToken)
	stats.imageTokensEst += s.geom.imageTokensFor(text)
	return blocks
}

func (p *PxpipePlugin) renderCached(text string, g geometry) [][]byte {
	if p.cache == nil {
		pngs, err := renderToPNGs(text, g)
		if err != nil {
			return nil
		}
		return pngs
	}
	key := renderKey(text, g)
	if pngs, ok := p.cache.get(key); ok {
		return pngs
	}
	pngs, err := renderToPNGs(text, g)
	if err != nil {
		return nil
	}
	p.cache.put(key, pngs)
	return pngs
}

func placeCacheBreakpoint(cr *schemas.BifrostChatRequest, lastImaged int) {
	if countCacheMarkers(cr) >= maxCacheBreakpoints {
		return
	}
	blocks := cr.Input[lastImaged].Content.ContentBlocks
	for j := len(blocks) - 1; j >= 0; j-- {
		if blocks[j].Type != schemas.ChatContentBlockTypeImage {
			continue
		}
		if blocks[j].CacheControl == nil {
			blocks[j].CacheControl = &schemas.CacheControl{Type: schemas.CacheControlTypeEphemeral}
		}
		return
	}
}

func countCacheMarkers(cr *schemas.BifrostChatRequest) int {
	count := 0
	for i := range cr.Input {
		c := cr.Input[i].Content
		if c == nil {
			continue
		}
		for k := range c.ContentBlocks {
			if c.ContentBlocks[k].CacheControl != nil {
				count++
			}
		}
	}
	return count
}
