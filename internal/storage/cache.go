package storage

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
	items map[uint64]*cacheEntry
	lru   *list.List
	mux   sync.RWMutex
	size  int
	max   int
}

func newCache(maxSize int) *cache {
	if maxSize < 0 {
		panic("NewCache: maxSize must be positive")
	}

	return &cache{
		items: make(map[uint64]*cacheEntry, maxSize),
		lru:   list.New(),
		max:   maxSize,
	}
}

func (c *cache) get(id uint64) (*model.LinkSet, bool) {
	c.mux.RLock()
	entry, ok := c.items[id]
	c.mux.RUnlock()

	if !ok {
		return nil, false
	}

	// Обновляем LRU (требует полной блокировки)
	c.mux.Lock()
	c.lru.MoveToFront(entry.elem)
	c.mux.Unlock()

	return entry.linkSet, true
}

func (c *cache) set(id uint64, linkSet *model.LinkSet) {
	c.mux.Lock()
	defer c.mux.Unlock()

	// Если уже есть - обновляем
	if entry, ok := c.items[id]; ok {
		entry.linkSet = linkSet
		c.lru.MoveToFront(entry.elem)
		return
	}

	// Eviction если нужно
	if c.size >= c.max && c.lru.Len() > 0 {
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
