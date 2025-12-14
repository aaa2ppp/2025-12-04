package bbolt

import (
	"testing"

	"github.com/aaa2ppp/be"

	"link-checker/internal/model"
)

func TestNewCache(t *testing.T) {
	t.Run("positive maxSize", func(t *testing.T) {
		cache := newCache(10)
		be.True(t, cache != nil)
		be.Equal(t, 0, cache.size)
		be.Equal(t, 10, cache.maxSize)
		be.True(t, cache.items != nil)
		be.True(t, cache.lru != nil)
	})

	t.Run("panic on zero maxSize", func(t *testing.T) {
		be.Err(t, panicToError(func() { newCache(0) }), "panic")
	})

	t.Run("panic on negative maxSize", func(t *testing.T) {
		be.Err(t, panicToError(func() { newCache(-5) }), "panic")
	})
}

func TestCache_Get(t *testing.T) {
	t.Run("nil receiver returns nil", func(t *testing.T) {
		var c *cache
		result := c.Get(1)
		be.True(t, result == nil)
	})

	t.Run("existing item", func(t *testing.T) {
		c := newCache(5)
		linkSet := &model.LinkSet{ID: 1}

		// Добавляем через внутренний метод для тестирования
		c.Do(func(lc lockedCache) {
			lc.Put(linkSet)
		})

		result := c.Get(1)
		be.Equal(t, linkSet, result)
	})

	t.Run("non-existing item returns nil", func(t *testing.T) {
		c := newCache(5)
		result := c.Get(999)
		be.True(t, result == nil)
	})

	t.Run("LRU order update", func(t *testing.T) {
		c := newCache(3)

		linkSet1 := &model.LinkSet{ID: 1}
		linkSet2 := &model.LinkSet{ID: 2}
		linkSet3 := &model.LinkSet{ID: 3}

		c.Do(func(lc lockedCache) {
			lc.Put(linkSet1)
			lc.Put(linkSet2)
			lc.Put(linkSet3)
		})

		// Доступ к элементу 2 должен переместить его в начало
		result := c.Get(2)
		be.Equal(t, linkSet2, result)

		// Проверяем порядок в LRU
		c.Do(func(lc lockedCache) {
			be.Equal(t, c.lru.Front().Value.(uint64), uint64(2))
		})
	})
}

func TestCache_Set(t *testing.T) {
	t.Run("panic on nil receiver", func(t *testing.T) {
		var c *cache
		be.Err(t, panicToError(func() { c.Put(&model.LinkSet{ID: 1}) }), "panic")
	})

	t.Run("add new item", func(t *testing.T) {
		c := newCache(3)
		linkSet := &model.LinkSet{ID: 1}

		c.Put(linkSet)

		result := c.Get(1)
		be.Equal(t, result, linkSet)
		be.Equal(t, c.size, 1)
	})

	t.Run("update existing item", func(t *testing.T) {
		c := newCache(3)
		linkSet1 := &model.LinkSet{ID: 1, Links: []model.Link{{Name: "Old"}}}
		linkSet2 := &model.LinkSet{ID: 1, Links: []model.Link{{Name: "New"}}}

		c.Put(linkSet1)
		c.Put(linkSet2)

		result := c.Get(1)
		be.Equal(t, result.Links[0].Name, "New")
		be.Equal(t, c.size, 1) // Размер не должен увеличиться
	})

	t.Run("LRU eviction when full", func(t *testing.T) {
		c := newCache(3)

		// Добавляем 3 элемента
		c.Put(&model.LinkSet{ID: 1})
		c.Put(&model.LinkSet{ID: 2})
		c.Put(&model.LinkSet{ID: 3})

		be.Equal(t, 3, c.size)

		// Проверяем, что все элементы на месте
		be.True(t, c.Get(1) != nil)
		be.True(t, c.Get(2) != nil)
		be.True(t, c.Get(3) != nil)

		// Добавляем 4-й элемент - должен вытеснить самый старый (1)
		c.Put(&model.LinkSet{ID: 4})

		be.Equal(t, 3, c.size)
		be.True(t, c.Get(1) == nil) // Элемент 1 должен быть удален
		be.True(t, c.Get(2) != nil)
		be.True(t, c.Get(3) != nil)
		be.True(t, c.Get(4) != nil)

		// Доступ к элементу 2 изменяет порядок LRU
		c.Get(2)

		// Добавляем 5-й элемент - должен вытеснить самый старый (3)
		c.Put(&model.LinkSet{ID: 5})

		be.True(t, c.Get(3) == nil) // Элемент 3 должен быть удален
		be.True(t, c.Get(2) != nil) // Элемент 2 должен остаться
		be.True(t, c.Get(4) != nil)
		be.True(t, c.Get(5) != nil)
	})

	t.Run("LRU update moves item to front", func(t *testing.T) {
		c := newCache(3)

		c.Put(&model.LinkSet{ID: 1})
		c.Put(&model.LinkSet{ID: 2})
		c.Put(&model.LinkSet{ID: 3})

		// Обновляем элемент 2
		c.Put(&model.LinkSet{ID: 2, Links: []model.Link{{Name: "Updated"}}})

		// Проверяем, что элемент 2 теперь в начале
		c.Do(func(lc lockedCache) {
			be.Equal(t, c.lru.Front().Value.(uint64), uint64(2))
			be.Equal(t, lc.Get(2).Links[0].Name, "Updated")
		})
	})
}

func TestCache_Do(t *testing.T) {
	t.Run("access with lockedCache", func(t *testing.T) {
		c := newCache(5)

		linkSet := &model.LinkSet{ID: 1, Links: []model.Link{{Name: "Test"}}}
		c.Put(linkSet)

		var accessedLinkSet *model.LinkSet
		c.Do(func(lc lockedCache) {
			// Проверяем Contains
			be.True(t, lc.Contains(1))
			be.True(t, !lc.Contains(999))

			// Проверяем Get через lockedCache
			accessedLinkSet = lc.Get(1)

			// Проверяем Set через lockedCache
			lc.Put(&model.LinkSet{ID: 2})
		})

		be.Equal(t, accessedLinkSet.Links[0].Name, "Test")
		be.True(t, c.Get(2) != nil)
	})

	t.Run("thread safety", func(t *testing.T) {
		c := newCache(100)

		// Запускаем несколько горутин
		done := make(chan bool)
		for i := 0; i < 10; i++ {
			go func(id int) {
				for j := 0; j < 100; j++ {
					linkID := uint64(id*100 + j)
					c.Put(&model.LinkSet{ID: linkID})
					c.Get(linkID)

					c.Do(func(lc lockedCache) {
						_ = lc.Contains(linkID)
					})
				}
				done <- true
			}(i)
		}

		// Ждем завершения всех горутин
		for i := 0; i < 10; i++ {
			<-done
		}

		// Проверяем, что кэш в валидном состоянии
		be.True(t, c.size <= c.maxSize)
		be.Equal(t, c.size, c.lru.Len())
	})
}

func TestLockedCache(t *testing.T) {
	t.Run("Contains works correctly", func(t *testing.T) {
		c := newCache(5)
		linkSet := &model.LinkSet{ID: 42}

		c.Do(func(lc lockedCache) {
			be.True(t, !lc.Contains(42))
			lc.Put(linkSet)
			be.True(t, lc.Contains(42))
			be.True(t, !lc.Contains(43))
		})
	})

	t.Run("Get through lockedCache", func(t *testing.T) {
		c := newCache(5)
		linkSet := &model.LinkSet{ID: 1, Links: []model.Link{{Name: "LockedTest"}}}

		c.Do(func(lc lockedCache) {
			be.True(t, lc.Get(1) == nil)
			lc.Put(linkSet)
			result := lc.Get(1)
			be.Equal(t, result.Links[0].Name, "LockedTest")
		})
	})

	t.Run("Set through lockedCache updates LRU", func(t *testing.T) {
		c := newCache(3)

		c.Do(func(lc lockedCache) {
			lc.Put(&model.LinkSet{ID: 1})
			lc.Put(&model.LinkSet{ID: 2})
			lc.Put(&model.LinkSet{ID: 3})

			// Доступ к элементу 1
			_ = lc.Get(1)

			// Добавляем новый элемент - должен вытеснить элемент 2
			lc.Put(&model.LinkSet{ID: 4})

			be.True(t, lc.Contains(1))
			be.True(t, !lc.Contains(2)) // Должен быть удален
			be.True(t, lc.Contains(3))
			be.True(t, lc.Contains(4))
		})
	})
}

func TestCache_EdgeCases(t *testing.T) {
	t.Run("single element cache", func(t *testing.T) {
		c := newCache(1)

		c.Put(&model.LinkSet{ID: 1})
		be.True(t, c.Get(1) != nil)

		c.Put(&model.LinkSet{ID: 2})
		be.True(t, c.Get(1) == nil)
		be.True(t, c.Get(2) != nil)
	})

	t.Run("concurrent access with Do", func(t *testing.T) {
		c := newCache(10)

		c.Do(func(lc lockedCache) {
			// Внутри Do мы должны иметь эксклюзивный доступ
			for i := uint64(0); i < 5; i++ {
				lc.Put(&model.LinkSet{ID: i})
			}

			// Проверяем, что все элементы добавлены
			for i := uint64(0); i < 5; i++ {
				be.True(t, lc.Contains(i))
			}
		})

		// Проверяем результаты после Do
		for i := uint64(0); i < 5; i++ {
			be.True(t, c.Get(i) != nil)
		}
	})
}
