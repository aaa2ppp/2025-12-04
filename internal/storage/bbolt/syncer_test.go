package bbolt

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/aaa2ppp/be"
)

func TestSyncer_DefaultConstants(t *testing.T) {
	// Проверяем, что константы имеют разумные значения
	be.True(t, DefaultMaxNotSyncedUpdates > 0)
	be.True(t, DefaultMaxSyncDelay > 0)
	be.True(t, DefaultSyncErrorLogPeriod > 0)

	// Проверяем соотношения
	be.True(t, DefaultMaxSyncDelay < DefaultSyncErrorLogPeriod)
}

func TestNewSyncer(t *testing.T) {
	t.Run("valid parameters", func(t *testing.T) {
		db := &mockDB{}
		logger := newTestLogger()

		syncer := newSyncer(db, logger, syncerConfig{
			maxNotSyncedUpdates: 100,
			maxSyncDelay:        2 * time.Second,
			syncErrorLogPeriod:  30 * time.Second,
		})

		be.True(t, syncer != nil)
		be.Equal(t, syncer.db.(*mockDB), db)
		be.Equal(t, syncer.logger, logger)
		be.Equal(t, syncer.maxNotSyncedUpdates, 100)
		be.Equal(t, syncer.maxSyncDelay, 2*time.Second)
		be.Equal(t, syncer.syncErrorLogPeriod, 30*time.Second)

		syncer.Close()
	})

	t.Run("zero values use defaults", func(t *testing.T) {
		syncer := newSyncer(&mockDB{}, nil, syncerConfig{})

		be.Equal(t, syncer.maxNotSyncedUpdates, DefaultMaxNotSyncedUpdates)
		be.Equal(t, syncer.maxSyncDelay, DefaultMaxSyncDelay)
		be.Equal(t, syncer.syncErrorLogPeriod, DefaultSyncErrorLogPeriod)

		syncer.Close()
	})

	t.Run("nil db panics", func(t *testing.T) {
		err := panicToError(func() {
			syncer := newSyncer(nil, nil, syncerConfig{})
			syncer.Close()
		})
		be.Err(t, err, "panic")
	})

	t.Run("negative values use defaults", func(t *testing.T) {
		syncer := newSyncer(&mockDB{}, nil, syncerConfig{
			maxNotSyncedUpdates: -100,
			maxSyncDelay:        -5 * time.Second,
			syncErrorLogPeriod:  -10 * time.Second,
		})

		be.Equal(t, syncer.maxNotSyncedUpdates, DefaultMaxNotSyncedUpdates)
		be.Equal(t, syncer.maxSyncDelay, DefaultMaxSyncDelay)
		be.Equal(t, syncer.syncErrorLogPeriod, DefaultSyncErrorLogPeriod)

		syncer.Close()
	})
}

func TestSyncer_Close(t *testing.T) {
	t.Run("nil receiver does nothing", func(t *testing.T) {
		var s *periodicSyncer
		// Не должно паниковать
		be.Err(t, panicToError(s.Close), nil)
	})

	t.Run("closes channel", func(t *testing.T) {
		syncer := newSyncer(&mockDB{}, newTestLogger(), syncerConfig{})

		// Даем время горутине запуститься
		time.Sleep(5 * time.Millisecond)

		// Закрываем
		syncer.Close()

		// Повторный вызов не должен паниковать
		be.Err(t, panicToError(syncer.Close), nil)
	})
}

func TestSyncer_Update(t *testing.T) {
	t.Run("nil receiver does nothing", func(t *testing.T) {
		var s *periodicSyncer
		// Не должно паниковать
		be.Err(t, panicToError(s.Update), nil)
	})
	t.Run("sends to update channel 1", func(t *testing.T) {
		var syncCount atomic.Int32
		db := &mockDB{
			syncFunc: func() error { syncCount.Add(1); return nil },
		}

		syncer := newSyncer(db, newTestLogger(), syncerConfig{
			maxNotSyncedUpdates: 1,
			maxSyncDelay:        100 * time.Millisecond,
		})
		defer syncer.Close()

		// Ждем запуска горутины
		time.Sleep(5 * time.Millisecond)

		syncer.Update()
		time.Sleep(5 * time.Millisecond)
		be.Equal(t, syncCount.Load(), 1)
	})

	t.Run("sends to update channel 2", func(t *testing.T) {
		var syncCount atomic.Int32
		db := &mockDB{
			syncFunc: func() error { syncCount.Add(1); return nil },
		}

		syncer := newSyncer(db, newTestLogger(), syncerConfig{
			maxNotSyncedUpdates: 1,
			maxSyncDelay:        100 * time.Millisecond,
		})
		defer syncer.Close()

		// Ждем запуска горутины
		time.Sleep(5 * time.Millisecond)

		syncer.Update()
		time.Sleep(5 * time.Millisecond)
		be.Equal(t, syncCount.Load(), 1)

		syncer.Update()
		time.Sleep(5 * time.Millisecond)
		be.Equal(t, syncCount.Load(), 2)
	})

	t.Run("sends to update channel - maxNotSyncedUpdates limit", func(t *testing.T) {
		var syncCount atomic.Int32
		db := &mockDB{
			syncFunc: func() error { syncCount.Add(1); return nil },
		}

		syncer := newSyncer(db, newTestLogger(), syncerConfig{
			maxNotSyncedUpdates: 10,
			maxSyncDelay:        200 * time.Millisecond,
		})
		defer syncer.Close()

		// Ждем запуска горутины
		time.Sleep(5 * time.Millisecond)

		for range 9 {
			syncer.Update()
			time.Sleep(5 * time.Millisecond)
			be.Equal(t, syncCount.Load(), 0)
		}

		syncer.Update()
		time.Sleep(5 * time.Millisecond)
		be.Equal(t, syncCount.Load(), 1)
	})

	t.Run("sends to update channel - maxSyncDelay limit", func(t *testing.T) {
		var syncCount atomic.Int32
		db := &mockDB{
			syncFunc: func() error { syncCount.Add(1); return nil },
		}
		syncer := newSyncer(db, newTestLogger(), syncerConfig{
			maxNotSyncedUpdates: 100,
			maxSyncDelay:        20 * time.Millisecond,
		})
		defer syncer.Close()

		// Ждем запуска горутины
		time.Sleep(5 * time.Millisecond)

		syncer.Update()
		time.Sleep(5 * time.Millisecond)
		be.Equal(t, syncCount.Load(), 0)

		time.Sleep(25 * time.Millisecond)
		be.Equal(t, syncCount.Load(), 1)
	})

	t.Run("ignores updates after close", func(t *testing.T) {
		var syncCount atomic.Int32
		db := &mockDB{
			syncFunc: func() error { syncCount.Add(1); return nil },
		}
		syncer := newSyncer(db, newTestLogger(), syncerConfig{
			maxNotSyncedUpdates: 1,
			maxSyncDelay:        100 * time.Millisecond,
		})

		// Сразу закрываем
		syncer.Close()

		// Даем время горутине завершиться
		time.Sleep(5 * time.Millisecond)

		// Обновление после закрытия не должно паниковать
		be.Err(t, panicToError(syncer.Update), nil)
		time.Sleep(5 * time.Millisecond)
		be.Equal(t, syncCount.Load(), 0)
	})

	t.Run("concurrent updates - maxNotSyncedUpdates limit", func(t *testing.T) {
		var syncCount atomic.Int32
		db := &mockDB{
			syncFunc: func() error { syncCount.Add(1); return nil },
		}
		syncer := newSyncer(db, newTestLogger(), syncerConfig{
			maxNotSyncedUpdates: 10,
			maxSyncDelay:        500 * time.Millisecond,
		})
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
					syncer.Update()
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
