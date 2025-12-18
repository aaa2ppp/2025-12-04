package bbolt

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"link-checker/internal/model"

	"github.com/aaa2ppp/be"
	"golang.org/x/sync/errgroup"
)

type stubBatchSaver struct{}

func (bs stubBatchSaver) saveBatch(batch [][]model.Link) ([]uint64, error) {
	return nil, nil
}

type fakeBatchSaver struct {
	data  [][]model.Link
	calls int
}

func (bs *fakeBatchSaver) saveBatch(batch [][]model.Link) ([]uint64, error) {
	bs.calls++
	ids := make([]uint64, 0, len(batch))
	for _, links := range batch {
		bs.data = append(bs.data, links)
		ids = append(ids, uint64(len(bs.data)))
	}
	return ids, nil
}

type mockBatchSaver struct {
	saveBatchFunc func(batch [][]model.Link) ([]uint64, error)
}

func (bs *mockBatchSaver) saveBatch(batch [][]model.Link) ([]uint64, error) {
	if bs.saveBatchFunc != nil {
		return bs.saveBatchFunc(batch)
	}
	return nil, nil
}

func TestSaver_Close(t *testing.T) {
	t.Run("close waits for serve to finish", func(t *testing.T) {
		bs := &stubBatchSaver{}
		saver := newBatchSaver(bs.saveBatch, 0, 10*time.Millisecond)

		ctx := context.Background()

		// Даем время горутине запуститься
		time.Sleep(5 * time.Millisecond)

		err := saver.Close(ctx)
		be.Err(t, err, nil)

		// Повторный вызов Close возвращает ошибку
		err = saver.Close(ctx)
		be.Err(t, err, ErrBatchSaverClosed)
	})

	t.Run("close saves pending queue", func(t *testing.T) {
		bs := &fakeBatchSaver{}

		saver := newBatchSaver(bs.saveBatch, 100, 100*time.Millisecond)

		// Даем время горутине запуститься
		time.Sleep(5 * time.Millisecond)

		// Отправляем запрос на сохранение
		ctx := context.Background()
		links := []model.Link{{Name: "Test", URL: "https://example.com"}}

		errCh := make(chan error, 1)
		go func() {
			_, err := saver.Save(ctx, links)
			errCh <- err
		}()

		// Ждем немного, чтобы вошли в Save
		time.Sleep(5 * time.Millisecond)

		// Закрываем saver - должен сохранить очередь
		saver.Close(ctx)

		// Ждем ответ от Save
		be.Err(t, <-errCh, nil)

		// Проверяем, что данные были сохранены
		be.Equal(t, bs.calls, 1)
		be.Equal(be.Require(t), len(bs.data), 1)
		be.Equal(t, bs.data[0], links)
	})
}

func TestPendingSaver_NoConcurrentSaveBatch(t *testing.T) {
	var (
		inProcess atomic.Bool
		calls     atomic.Int32
	)

	bs := &mockBatchSaver{
		saveBatchFunc: func(batch [][]model.Link) ([]uint64, error) {
			calls.Add(1)
			if !inProcess.CompareAndSwap(false, true) {
				panic("SaveBatch called concurrently!")
			}
			defer inProcess.Store(false)
			time.Sleep(10 * time.Millisecond)
			return make([]uint64, len(batch)), nil
		},
	}

	saver := newBatchSaver(bs.saveBatch, 1000, 10*time.Millisecond)
	defer saver.Close(context.Background())

	var mu sync.Mutex
	minimum := time.Now().Add(100500 * time.Second)
	maximum := time.Time{}

	const N = 10000
	err := func() (err error) {
		defer func() {
			if p := recover(); p != nil {
				err = fmt.Errorf("panic: %v", p)
			}
		}()

		var eg errgroup.Group
		for i := range N {
			i := i // да перестанут в меня тыкать! (если что, то в go.mod v1.24)
			eg.Go(func() error {
				time.Sleep(time.Duration(i%1000) * time.Millisecond)
				mu.Lock()
				t := time.Now()
				if t.Before(minimum) {
					minimum = t
				}
				if t.After(maximum) {
					maximum = t
				}
				mu.Unlock()
				_, err := saver.Save(context.Background(), nil)
				return err
			})
		}
		be.Err(t, eg.Wait(), nil)

		return nil
	}()

	t.Logf("elapsed: %v calls: %v", maximum.Sub(minimum), calls.Load())
	be.True(t, calls.Load() > 0)
	be.Err(t, err, nil)
}

func TestSaver_Save(t *testing.T) {
	t.Run("successful save", func(t *testing.T) {
		bs := &fakeBatchSaver{}

		ctx := context.Background()

		saver := newBatchSaver(bs.saveBatch, 100, 100*time.Millisecond)
		defer saver.Close(ctx)

		// Даем время горутине запуститься
		time.Sleep(5 * time.Millisecond)

		// Сохраняем данные
		links := []model.Link{{Name: "Test", URL: "https://example.com"}}

		_, err := saver.Save(ctx, links)
		be.Err(t, err, nil)

		// Ждем обработки
		time.Sleep(110 * time.Millisecond)

		// Проверяем, что данные были сохранены
		be.Equal(t, bs.calls, 1)
	})

	t.Run("save with context cancellation before send", func(t *testing.T) {
		bs := fakeBatchSaver{}

		ctx := context.Background()

		saver := newBatchSaver(bs.saveBatch, 1, 1)
		defer saver.Close(ctx)

		// Даем время горутине запуститься
		time.Sleep(5 * time.Millisecond)

		// Создаем контекст с отменой
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		links := []model.Link{{Name: "Test", URL: "https://example.com"}}
		_, err := saver.Save(ctx, links)
		be.Err(t, err, context.Canceled)
	})

	t.Run("save with context cancellation after send", func(t *testing.T) {
		// Этот тест проверяет, что если контекст отменяется после отправки в канал,
		// но до получения ответа, то возвращается ошибка контекста
		bs := &fakeBatchSaver{}

		ctx := context.Background()

		saver := newBatchSaver(bs.saveBatch, 100, 500*time.Millisecond) // Долгий таймаут
		defer saver.Close(ctx)

		// Даем время горутине запуститься
		time.Sleep(5 * time.Millisecond)

		// Создаем контекст с таймаутом
		ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
		defer cancel()

		links := []model.Link{{Name: "Test", URL: "https://example.com"}}
		_, err := saver.Save(ctx, links)
		be.Err(t, err, context.DeadlineExceeded)
	})

	t.Run("save after close returns error", func(t *testing.T) {
		bs := &fakeBatchSaver{}

		ctx := context.Background()

		saver := newBatchSaver(bs.saveBatch, 1, 1)

		// Закрываем сразу
		saver.Close(ctx)

		links := []model.Link{{Name: "Test", URL: "https://example.com"}}
		_, err := saver.Save(ctx, links)
		be.Err(t, err, ErrBatchSaverClosed)
	})

	t.Run("batch save on queue size limit", func(t *testing.T) {
		bs := &fakeBatchSaver{}

		ctx := context.Background()

		// Маленький размер очереди для теста
		saver := newBatchSaver(bs.saveBatch, 3, 500*time.Millisecond) // Долгий таймаут
		defer saver.Close(ctx)

		// Даем время горутине запуститься
		time.Sleep(5 * time.Millisecond)

		// Отправляем 3 запроса - должен сработать лимит очереди
		links := []model.Link{{Name: "Test", URL: "https://example.com"}}

		for i := 0; i < 3; i++ {
			go func() {
				saver.Save(ctx, links)
			}()
		}

		// Ждем обработки
		time.Sleep(100 * time.Millisecond)

		be.Equal(t, bs.calls, 1)
		be.Equal(t, len(bs.data), 3)
	})

	t.Run("batch save on timeout", func(t *testing.T) {
		bs := &fakeBatchSaver{}

		ctx := context.Background()

		// Короткий таймаут для теста
		saver := newBatchSaver(bs.saveBatch, 100, 20*time.Millisecond)
		defer saver.Close(ctx)

		// Даем время горутине запуститься
		time.Sleep(5 * time.Millisecond)

		// Отправляем один запрос
		links := []model.Link{{Name: "Test", URL: "https://example.com"}}

		go func() {
			saver.Save(ctx, links)
		}()

		// Ждем срабатывания таймаута
		time.Sleep(30 * time.Millisecond)

		// Должен быть вызов Update
		be.Equal(t, bs.calls, 1)
	})

	t.Run("database error propagates", func(t *testing.T) {
		bs := &mockBatchSaver{
			saveBatchFunc: func(batch [][]model.Link) ([]uint64, error) {
				return nil, errors.New("unknown error")
			},
		}

		ctx := context.Background()

		saver := newBatchSaver(bs.saveBatch, 100, 10*time.Millisecond)
		defer saver.Close(ctx)

		// Даем время горутине запуститься
		time.Sleep(5 * time.Millisecond)

		links := []model.Link{{Name: "Test", URL: "https://example.com"}}

		_, err := saver.Save(ctx, links)
		be.Err(t, err, "unknown error")
	})
}

func TestSaver_ServeQueue(t *testing.T) {
	t.Run("multiple concurrent saves", func(t *testing.T) {
		bs := &fakeBatchSaver{}

		ctx := context.Background()

		saver := newBatchSaver(bs.saveBatch, 10, 100*time.Millisecond)
		defer saver.Close(ctx)

		// Даем время горутине запуститься
		time.Sleep(5 * time.Millisecond)

		// Запускаем несколько горутин с сохранением
		const numSaves = 5
		results := make(chan uint64, numSaves)
		errors := make(chan error, numSaves)

		for i := 0; i < numSaves; i++ {
			go func(idx int) {
				links := []model.Link{{Name: "Test", URL: "https://example.com"}}
				id, err := saver.Save(ctx, links)
				if err == nil {
					results <- id
				} else {
					errors <- err
				}
			}(i)
		}

		// Ждем обработки (таймаут сработает через 100ms)
		time.Sleep(150 * time.Millisecond)

		// Собираем результаты
		var ids []uint64
		for i := 0; i < numSaves; i++ {
			select {
			case id := <-results:
				ids = append(ids, id)
			case err := <-errors:
				t.Errorf("unexpected error: %v", err)
			}
		}

		// Проверяем, что все ID разные и в правильном диапазоне
		be.Equal(t, len(ids), numSaves)

		// Проверяем, что все ID были обработаны
		be.Equal(t, len(bs.data), numSaves)
	})

	t.Run("empty queue does nothing", func(t *testing.T) {
		bs := &fakeBatchSaver{}

		ctx := context.Background()

		saver := newBatchSaver(bs.saveBatch, 100, 10*time.Millisecond)
		defer saver.Close(ctx)

		// Даем время горутине запуститься
		time.Sleep(20 * time.Millisecond)

		// Таймер сработает, но очередь пуста - update не должен вызываться
		be.Equal(t, bs.calls, 0)
	})
}
