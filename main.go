package pxpipe

import (
	"fmt"
	"sort"
	"strings"
	"sync/atomic"

	"github.com/maximhq/bifrost/core/schemas"
)

const PluginName = "pxpipe"

var _ schemas.LLMPlugin = (*PxpipePlugin)(nil)

var defaultModels = []string{"claude-fable-5", "gpt-5.6"}

const (
	defaultMinChars           = 2000
	defaultKeepRecentMessages = 4
	defaultMaxImages          = 100
	defaultRenderCacheSize    = 128

	defaultOutputInputPriceRatio = 5.0
	defaultDecodeTaxTokens       = 500.0
	defaultHorizon               = 8
)

type ProfileConfig struct {
	PatchPx               *int     `json:"patch_px,omitempty"`
	Cols                  *int     `json:"cols,omitempty"`
	MinChars              *int     `json:"min_chars,omitempty"`
	CharsPerToken         *float64 `json:"chars_per_token,omitempty"`
	OutputInputPriceRatio *float64 `json:"output_input_price_ratio,omitempty"`
	DecodeTaxTokens       *float64 `json:"decode_tax_tokens,omitempty"`
	Horizon               *int     `json:"horizon,omitempty"`
}

type Config struct {
	Enabled bool `json:"enabled"`

	Models []string `json:"models"`

	ModelProfiles map[string]ProfileConfig `json:"model_profiles"`

	MinChars int `json:"min_chars"`

	Cols int `json:"cols"`

	KeepRecentMessages int `json:"keep_recent_messages"`

	MaxImages int `json:"max_images"`

	CharsPerToken float64 `json:"chars_per_token"`

	PatchPx int `json:"patch_px"`

	OutputInputPriceRatio float64 `json:"output_input_price_ratio"`

	DecodeTaxTokens float64 `json:"decode_tax_tokens"`

	Horizon int `json:"horizon"`

	Classify *bool `json:"classify"`

	CacheBreakpoint *bool `json:"cache_breakpoint"`

	InkCycle bool `json:"ink_cycle"`

	RepeatLines int `json:"repeat_lines"`

	RenderCacheSize int `json:"render_cache_size"`
}

type settings struct {
	geom          geometry
	minChars      int
	charsPerToken float64
	classify      bool
	outRatio      float64
	decodeTax     float64
	horizon       int
	cacheBreak    bool
}

type profileSettings struct {
	pattern string
	set     settings
}

type PxpipePlugin struct {
	config   Config
	base     settings
	profiles []profileSettings
	cache    *renderCache
	logger   schemas.Logger

	requestsSeen        int64
	requestsTransformed int64
	imagesProduced      int64
	tokensSaved         int64
}

func boolVal(p *bool, def bool) bool {
	if p == nil {
		return def
	}
	return *p
}

func buildSettings(c Config) settings {
	patch := c.PatchPx
	if patch == 0 {
		patch = defaultPatchPx
	}
	if patch < 0 {
		patch = 0
	}
	cols := c.Cols
	if cols <= 0 {
		cols = defaultCols
	}
	if maxCols := maxWidthPx / cellW; cols > maxCols {
		cols = maxCols
	}
	g := newGeometry(cols, patch)
	if c.RepeatLines > 1 {
		g.repeat = c.RepeatLines
	}
	g.inkCycle = c.InkCycle

	minChars := c.MinChars
	if minChars <= 0 {
		minChars = defaultMinChars
	}
	cpt := c.CharsPerToken
	if cpt <= 0 {
		cpt = defaultGateCharsPerToken
	}
	outRatio := c.OutputInputPriceRatio
	if outRatio <= 0 {
		outRatio = defaultOutputInputPriceRatio
	}
	decodeTax := c.DecodeTaxTokens
	switch {
	case decodeTax == 0:
		decodeTax = defaultDecodeTaxTokens
	case decodeTax < 0:
		decodeTax = 0
	}
	horizon := c.Horizon
	switch {
	case horizon == 0:
		horizon = defaultHorizon
	case horizon < 0:
		horizon = 0
	}

	return settings{
		geom:          g,
		minChars:      minChars,
		charsPerToken: cpt,
		classify:      boolVal(c.Classify, true),
		outRatio:      outRatio,
		decodeTax:     decodeTax,
		horizon:       horizon,
		cacheBreak:    boolVal(c.CacheBreakpoint, true),
	}
}

func overlay(base Config, p ProfileConfig) Config {
	c := base
	if p.PatchPx != nil {
		c.PatchPx = *p.PatchPx
	}
	if p.Cols != nil {
		c.Cols = *p.Cols
	}
	if p.MinChars != nil {
		c.MinChars = *p.MinChars
	}
	if p.CharsPerToken != nil {
		c.CharsPerToken = *p.CharsPerToken
	}
	if p.OutputInputPriceRatio != nil {
		c.OutputInputPriceRatio = *p.OutputInputPriceRatio
	}
	if p.DecodeTaxTokens != nil {
		c.DecodeTaxTokens = *p.DecodeTaxTokens
	}
	if p.Horizon != nil {
		c.Horizon = *p.Horizon
	}
	return c
}

func Init(config Config, logger schemas.Logger) (*PxpipePlugin, error) {
	if len(config.Models) == 0 && len(config.ModelProfiles) == 0 {
		config.Models = defaultModels
	}
	if config.KeepRecentMessages <= 0 {
		config.KeepRecentMessages = defaultKeepRecentMessages
	}
	if config.MaxImages <= 0 {
		config.MaxImages = defaultMaxImages
	}
	cacheSize := config.RenderCacheSize
	switch {
	case cacheSize == 0:
		cacheSize = defaultRenderCacheSize
	case cacheSize < 0:
		cacheSize = 0
	}

	base := buildSettings(config)

	profiles := make([]profileSettings, 0, len(config.ModelProfiles))
	for pattern, pc := range config.ModelProfiles {
		profiles = append(profiles, profileSettings{pattern: pattern, set: buildSettings(overlay(config, pc))})
	}

	sort.Slice(profiles, func(i, j int) bool {
		if len(profiles[i].pattern) != len(profiles[j].pattern) {
			return len(profiles[i].pattern) > len(profiles[j].pattern)
		}
		return profiles[i].pattern < profiles[j].pattern
	})

	return &PxpipePlugin{
		config:   config,
		base:     base,
		profiles: profiles,
		cache:    newRenderCache(cacheSize),
		logger:   logger,
	}, nil
}

func (p *PxpipePlugin) GetName() string { return PluginName }

func (p *PxpipePlugin) Cleanup() error { return nil }

func (p *PxpipePlugin) settingsFor(model string) settings {
	m := strings.ToLower(model)
	for _, ps := range p.profiles {
		if ps.pattern != "" && strings.Contains(m, strings.ToLower(ps.pattern)) {
			return ps.set
		}
	}
	return p.base
}

func (p *PxpipePlugin) modelAllowed(model string) bool {
	if model == "" {
		return false
	}
	m := strings.ToLower(model)
	for _, pat := range p.config.Models {
		if pat != "" && strings.Contains(m, strings.ToLower(pat)) {
			return true
		}
	}
	for _, ps := range p.profiles {
		if ps.pattern != "" && strings.Contains(m, strings.ToLower(ps.pattern)) {
			return true
		}
	}
	return false
}

func (p *PxpipePlugin) PreRequestHook(_ *schemas.BifrostContext, req *schemas.BifrostRequest) error {
	if !p.config.Enabled || req == nil || req.ChatRequest == nil {
		return nil
	}
	cr := req.ChatRequest
	atomic.AddInt64(&p.requestsSeen, 1)
	if !p.modelAllowed(cr.Model) {
		return nil
	}

	stats := p.transformChat(cr, p.settingsFor(cr.Model))
	if !stats.touched() {
		return nil
	}
	atomic.AddInt64(&p.requestsTransformed, 1)
	atomic.AddInt64(&p.imagesProduced, int64(stats.imagesProduced))
	atomic.AddInt64(&p.tokensSaved, int64(stats.tokensSaved()))
	if p.logger != nil {
		p.logger.Debug(fmt.Sprintf(
			"[pxpipe] model=%s imaged %d block(s) into %d image(s); ~%d text tokens -> ~%d image tokens (net ~%d saved)",
			cr.Model, stats.blocksImaged, stats.imagesProduced,
			stats.textTokensEst, stats.imageTokensEst, stats.tokensSaved(),
		))
	}
	return nil
}

func (p *PxpipePlugin) PreLLMHook(_ *schemas.BifrostContext, req *schemas.BifrostRequest) (*schemas.BifrostRequest, *schemas.LLMPluginShortCircuit, error) {
	return req, nil, nil
}

func (p *PxpipePlugin) PostLLMHook(_ *schemas.BifrostContext, resp *schemas.BifrostResponse, bifrostErr *schemas.BifrostError) (*schemas.BifrostResponse, *schemas.BifrostError, error) {
	return resp, bifrostErr, nil
}
