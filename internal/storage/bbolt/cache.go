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

	items := make(map[uint64]cacheEntry, maxSize)
	lru := list.New()

	return &cache{
		items:   items,
		lru:     lru,
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

	return c.get(id)
}

func (c *cache) get(id uint64) *model.LinkSet {
	if entry, ok := c.items[id]; ok {
		c.lru.MoveToFront(entry.elem)
		return entry.linkSet
	}
	return nil
}

// Put обновляет/добавляет запись в кэше. Паникует если ресивер nil.
// Поднимает приоритет записи в кэше.
// При необходимости удаляет низкоприоритетные записи.
func (c *cache) Put(linkSet *model.LinkSet) {
	if c == nil {
		panic("cache.Set on <nil>")
	}

	c.mux.Lock()
	defer c.mux.Unlock()

	c.set(linkSet.ID, linkSet)
}

func (c *cache) set(id uint64, linkSet *model.LinkSet) {
	var elem *list.Element

	if entry, ok := c.items[id]; ok {
		elem = entry.elem
		c.lru.MoveToFront(elem)
	} else {
		if c.size >= c.maxSize { // конструктор гарантирует, maxSize > 0
			oldest := c.lru.Back()
			c.lru.Remove(oldest)
			oldID := oldest.Value.(uint64)
			delete(c.items, oldID)
			c.size--
		}
		elem = c.lru.PushFront(id)
		c.size++
	}

	c.items[id] = cacheEntry{
		linkSet: linkSet,
		elem:    elem,
	}
}

type lockedCache struct {
	cache *cache
}

func (u lockedCache) Contains(id uint64) bool {
	_, ok := u.cache.items[id]
	return ok
}

func (u lockedCache) Get(id uint64) *model.LinkSet {
	return u.cache.get(id)
}

func (u lockedCache) Put(linkSet *model.LinkSet) {
	u.cache.set(linkSet.ID, linkSet)
}

func (c *cache) Do(fn func(lockedCache)) {
	c.mux.Lock()
	defer c.mux.Unlock()

	fn(lockedCache{c})
}
