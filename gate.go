package pxpipe

import (
	"math"
	"strings"
	"unicode/utf8"
)

const (
	pixelsPerToken = 750.0

	imageCostSafetyMargin = 1.10

	defaultGateCharsPerToken = 4.0

	reportCharsPerToken = 3.7

	structuredCharsPerToken = 2.5
	logCharsPerToken        = 3.0
)

type contentClass int

const (
	classProse contentClass = iota
	classLog
	classStructured
)

func classifyContent(text string) contentClass {
	s := text
	if len(s) > 4096 {
		s = s[:4096]
	}
	if len(s) == 0 {
		return classProse
	}
	trimmed := strings.TrimLeft(s, " \t\r\n")
	if strings.HasPrefix(trimmed, "{") || strings.HasPrefix(trimmed, "[") {
		return classStructured
	}
	punct := 0
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '{', '}', '[', ']', ':', ',', '"', '=', ';', '<', '>', '(', ')':
			punct++
		}
	}
	if punct*100 >= len(s)*6 {
		return classStructured
	}
	lines := strings.Split(s, "\n")
	logish := 0
	for _, ln := range lines {
		if looksLikeLogLine(ln) {
			logish++
		}
	}
	if len(lines) >= 4 && logish*2 >= len(lines) {
		return classLog
	}
	return classProse
}

func looksLikeLogLine(ln string) bool {
	for i := 0; i+4 < len(ln); i++ {
		if isDigit(ln[i]) && isDigit(ln[i+1]) && ln[i+2] == ':' && isDigit(ln[i+3]) && isDigit(ln[i+4]) {
			return true
		}
	}
	for _, kw := range []string{"ERROR", "WARN", "INFO", "DEBUG", "TRACE"} {
		if strings.Contains(ln, kw) {
			return true
		}
	}
	return false
}

func isDigit(b byte) bool { return b >= '0' && b <= '9' }

func gateCPT(text string, classify bool, base float64) float64 {
	if base <= 0 {
		base = defaultGateCharsPerToken
	}
	if !classify {
		return base
	}
	switch classifyContent(text) {
	case classStructured:
		return structuredCharsPerToken
	case classLog:
		return logCharsPerToken
	default:
		return base
	}
}

func (g geometry) imageTokens(rows int) int {
	if rows <= 0 {
		return 0
	}
	w := g.pageWidthPx()
	total := 0.0
	for start := 0; start < rows; start += g.rowsPerPage {
		pageRows := g.rowsPerPage
		if rows-start < pageRows {
			pageRows = rows - start
		}
		total += float64(w * g.pageHeightPx(pageRows))
	}
	return int(math.Ceil(total / pixelsPerToken * imageCostSafetyMargin))
}

func (g geometry) imageTokensFor(text string) int {
	eff, rows := g.plan(text)
	return eff.imageTokens(len(rows))
}

func profitable(text string, s settings) bool {
	n := utf8.RuneCountInString(text)
	if n == 0 {
		return false
	}
	img := float64(s.geom.imageTokensFor(text))
	textTokens := float64(n) / gateCPT(text, s.classify, s.charsPerToken)
	if img >= textTokens {
		return false
	}
	decodeTax := s.decodeTax * s.outRatio
	if s.horizon > 0 {
		decodeTax /= float64(s.horizon)
	}
	return (textTokens - img) > decodeTax
}
