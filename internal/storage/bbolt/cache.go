package bbolt

import (
	"container/list"
	"sync"

	"link-checker/internal/model"
)

type cacheEntry struct {
	linkSet *model.LinkSet
	elem    *list.Element
}

type cache struct {
	items   map[uint64]*cacheEntry
	lru     *list.List
	mux     sync.Mutex
	size    int
	maxSize int
}

func newCache(maxSize int) *cache {
	if maxSize <= 0 {
		panic("newCache: maxSize must be positive")
	}

	return &cache{
		items:   make(map[uint64]*cacheEntry, maxSize),
		lru:     list.New(),
		maxSize: maxSize,
	}
}

func (c *cache) Get(id uint64) (*model.LinkSet, bool) {
	if c == nil {
		return nil, false
	}

	c.mux.Lock()
	defer c.mux.Unlock()

	if entry, ok := c.items[id]; ok {
		c.lru.MoveToFront(entry.elem)
		return entry.linkSet, true
	}

	return nil, false
}

func (c *cache) Set(id uint64, linkSet *model.LinkSet) {
	if c == nil {
		panic("cache.set on <nil>")
	}
	c.mux.Lock()
	defer c.mux.Unlock()

	// Если уже есть - обновляем
	if entry, ok := c.items[id]; ok {
		entry.linkSet = linkSet
		c.lru.MoveToFront(entry.elem)
		return
	}

	// Eviction если нужно
	if c.size >= c.maxSize && c.lru.Len() > 0 {
		oldest := c.lru.Back()
		c.lru.Remove(oldest)
		oldID := oldest.Value.(uint64)
		delete(c.items, oldID)
		c.size--
	}

	// Добавляем новый
	elem := c.lru.PushFront(id)
	c.items[id] = &cacheEntry{
		linkSet: linkSet,
		elem:    elem,
	}
	c.size++
}
