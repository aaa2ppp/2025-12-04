package bbolt

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/aaa2ppp/be"
)

type mockSyncer struct {
	syncFunc func() error
}

func (s *mockSyncer) Sync() error {
	if s.syncFunc != nil {
		return s.syncFunc()
	}
	return nil
}

func TestSyncer_Close(t *testing.T) {
	t.Run("closes channel", func(t *testing.T) {
		db := &mockSyncer{}
		syncer := newLazySyncer(db.Sync, 1, 1)

		// Даем время горутине запуститься
		time.Sleep(5 * time.Millisecond)

		// Закрываем
		syncer.Close()

		// Повторный вызов не должен паниковать
		be.Err(t, panicToError(syncer.Close), nil)
	})
}

func TestSyncer_Update(t *testing.T) {
	t.Run("sends to update channel 1", func(t *testing.T) {
		var syncCount atomic.Int32
		db := &mockSyncer{
			syncFunc: func() error { syncCount.Add(1); return nil },
		}

		syncer := newLazySyncer(db.Sync, 1, 100*time.Millisecond)
		defer syncer.Close()

		// Ждем запуска горутины
		time.Sleep(5 * time.Millisecond)

		syncer.Update(1)
		time.Sleep(5 * time.Millisecond)
		be.Equal(t, syncCount.Load(), 1)
	})

	t.Run("sends to update channel 2", func(t *testing.T) {
		var syncCount atomic.Int32
		db := &mockSyncer{
			syncFunc: func() error { syncCount.Add(1); return nil },
		}

		syncer := newLazySyncer(db.Sync, 1, 100*time.Millisecond)
		defer syncer.Close()

		// Ждем запуска горутины
		time.Sleep(5 * time.Millisecond)

		syncer.Update(1)
		time.Sleep(5 * time.Millisecond)
		be.Equal(t, syncCount.Load(), 1)

		syncer.Update(1)
		time.Sleep(5 * time.Millisecond)
		be.Equal(t, syncCount.Load(), 2)
	})

	t.Run("sends to update channel - maxPending limit", func(t *testing.T) {
		var syncCount atomic.Int32
		db := &mockSyncer{
			syncFunc: func() error { syncCount.Add(1); return nil },
		}

		syncer := newLazySyncer(db.Sync, 10, 200*time.Millisecond)
		defer syncer.Close()

		// Ждем запуска горутины
		time.Sleep(5 * time.Millisecond)

		for range 9 {
			syncer.Update(1)
			time.Sleep(5 * time.Millisecond)
			be.Equal(t, syncCount.Load(), 0)
		}

		syncer.Update(1)
		time.Sleep(5 * time.Millisecond)
		be.Equal(t, syncCount.Load(), 1)
	})

	t.Run("sends to update channel - maxDelay limit", func(t *testing.T) {
		var syncCount atomic.Int32
		db := &mockSyncer{
			syncFunc: func() error { syncCount.Add(1); return nil },
		}
		syncer := newLazySyncer(db.Sync, 100, 20*time.Millisecond)
		defer syncer.Close()

		// Ждем запуска горутины
		time.Sleep(5 * time.Millisecond)

		syncer.Update(1)
		time.Sleep(5 * time.Millisecond)
		be.Equal(t, syncCount.Load(), 0)

		time.Sleep(25 * time.Millisecond)
		be.Equal(t, syncCount.Load(), 1)
	})

	t.Run("ignores updates after close", func(t *testing.T) {
		var syncCount atomic.Int32
		db := &mockSyncer{
			syncFunc: func() error { syncCount.Add(1); return nil },
		}
		syncer := newLazySyncer(db.Sync, 1, 100*time.Millisecond)

		// Сразу закрываем
		syncer.Close()

		// Даем время горутине завершиться
		time.Sleep(5 * time.Millisecond)

		// Обновление после закрытия не должно паниковать
		be.Err(t, panicToError(func() { syncer.Update(1) }), nil)
		time.Sleep(5 * time.Millisecond)
		be.Equal(t, syncCount.Load(), 0)
	})

	t.Run("concurrent updates - maxPending limit", func(t *testing.T) {
		var syncCount atomic.Int32
		db := &mockSyncer{
			syncFunc: func() error { syncCount.Add(1); return nil },
		}
		syncer := newLazySyncer(db.Sync, 10, 500*time.Millisecond)
		defer syncer.Close()

		// Ждем запуска горутины
		time.Sleep(5 * time.Millisecond)

		// Запускаем несколько горутин с обновлениями
		var wg sync.WaitGroup
		wg.Add(5)
		for i := 0; i < 5; i++ {
			go func() {
				defer wg.Done()
				for j := 0; j < 10; j++ {
					syncer.Update(1)
					time.Sleep(time.Duration(j) * time.Millisecond)
				}
			}()
		}

		// Ждем завершения всех горутин
		wg.Wait()

		// Ждем завершения всех синхронизаций
		time.Sleep(5 * time.Millisecond)

		be.Equal(t, syncCount.Load(), 5)
	})
}

// TODO: "sync error logging throttling"
