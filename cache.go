package pxpipe

import (
	"container/list"
	"crypto/sha256"
	"encoding/binary"
	"sync"
)

type renderCache struct {
	mu  sync.Mutex
	max int
	ll  *list.List
	m   map[string]*list.Element
}

type cacheEntry struct {
	key  string
	pngs [][]byte
}

func newRenderCache(max int) *renderCache {
	if max <= 0 {
		return nil
	}
	return &renderCache{max: max, ll: list.New(), m: make(map[string]*list.Element, max)}
}

func (c *renderCache) get(key string) ([][]byte, bool) {
	if c == nil {
		return nil, false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if e, ok := c.m[key]; ok {
		c.ll.MoveToFront(e)
		return e.Value.(*cacheEntry).pngs, true
	}
	return nil, false
}

func (c *renderCache) put(key string, pngs [][]byte) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if e, ok := c.m[key]; ok {
		c.ll.MoveToFront(e)
		e.Value.(*cacheEntry).pngs = pngs
		return
	}
	c.m[key] = c.ll.PushFront(&cacheEntry{key: key, pngs: pngs})
	for c.ll.Len() > c.max {
		back := c.ll.Back()
		if back == nil {
			break
		}
		c.ll.Remove(back)
		delete(c.m, back.Value.(*cacheEntry).key)
	}
}

func renderKey(text string, g geometry) string {
	h := sha256.New()
	var hdr [8]byte
	for _, v := range []int{g.cols, g.cellW, g.cellH, g.padX, g.padY, g.patchPx, g.rowsPerPage, g.repeat} {
		binary.LittleEndian.PutUint64(hdr[:], uint64(v))
		h.Write(hdr[:])
	}
	if g.inkCycle {
		h.Write([]byte{1})
	} else {
		h.Write([]byte{0})
	}
	h.Write([]byte(text))
	return string(h.Sum(nil))
}
