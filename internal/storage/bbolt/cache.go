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
	items   map[uint64]cacheEntry
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
		items:   make(map[uint64]cacheEntry, maxSize),
		lru:     list.New(),
		maxSize: maxSize,
	}
}

// Get возвращает запись из кэша. Если запись не найдена или ресивер nil, возвращает nil.
// Поднимает актуальность записи в кэше.
func (c *cache) Get(id uint64) *model.LinkSet {
	if c == nil {
		return nil
	}

	c.mux.Lock()
	defer c.mux.Unlock()

	if entry, ok := c.items[id]; ok {
		c.lru.MoveToFront(entry.elem)
		return entry.linkSet
	}

	return nil
}

// Set обновляет/добавляет запись в кэше. Паникует если ресивер nil.
// Поднимает приоритет записи в кэше.
// При необходимости удаляет низкоприоритетные записи.
func (c *cache) Set(linkSet *model.LinkSet) {
	if c == nil {
		panic("cache.Set on <nil>")
	}

	c.mux.Lock()
	defer c.mux.Unlock()

	c.set(linkSet.ID, linkSet)
}

func (c *cache) set(id uint64, linkSet *model.LinkSet) {
	var elem *list.Element

	if empty, ok := c.items[id]; ok {
		elem = empty.elem
		c.lru.MoveToFront(elem)
	} else {
		elem = c.lru.PushFront(id)
	}

	for c.size >= c.maxSize && c.lru.Len() > 0 {
		oldest := c.lru.Back()
		c.lru.Remove(oldest)
		oldID := oldest.Value.(uint64)
		delete(c.items, oldID)
		c.size--
	}

	c.items[id] = cacheEntry{
		linkSet: linkSet,
		elem:    elem,
	}
	c.size++
}

type lockedCache struct {
	cache *cache
}

func (u lockedCache) Contains(id uint64) bool {
	_, ok := u.cache.items[id]
	return ok
}

func (u lockedCache) Get(id uint64) *model.LinkSet {
	if entry, ok := u.cache.items[id]; ok {
		u.cache.lru.MoveToFront(entry.elem)
		return entry.linkSet
	}
	return nil
}

func (u lockedCache) Set(linkSet *model.LinkSet) {
	u.cache.set(linkSet.ID, linkSet)
}

func (c *cache) Do(fn func(lockedCache)) {
	c.mux.Lock()
	defer c.mux.Unlock()

	fn(lockedCache{c})
}
