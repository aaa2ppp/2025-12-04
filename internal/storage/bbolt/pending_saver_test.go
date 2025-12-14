package bbolt

import (
	"context"
	"errors"
	"testing"
	"time"

	"link-checker/internal/model"

	"github.com/aaa2ppp/be"
)

type testBatchSaver struct {
	count int
}

func (bs *testBatchSaver) saveBatch(ctx context.Context, batch [][]model.Link) ([]uint64, error) {
	ids := make([]uint64, len(batch))
	for range batch {
		bs.count++
		ids = append(ids, uint64(bs.count))
	}
	return ids, nil
}

func TestSaver_Close(t *testing.T) {
	t.Run("close waits for serve to finish", func(t *testing.T) {
		var bs testBatchSaver

		saver := newPendingSaver(bs.saveBatch, pendingSaverConfig{
			Delay: 10 * time.Millisecond,
		})

		// Даем время горутине запуститься
		time.Sleep(5 * time.Millisecond)

		err := saver.Close()
		be.Err(t, err, nil)

		// Повторный вызов Close возвращает ошибку
		err = saver.Close()
		be.Err(t, err, ErrSaverClosed)
	})

	t.Run("close saves pending queue", func(t *testing.T) {
		var bs testBatchSaver

		saver := newPendingSaver(bs.saveBatch, pendingSaverConfig{
			QueueSize: 100,
			Delay:     100 * time.Millisecond,
		})

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
		saver.Close()

		// Ждем ответ от Save
		be.Err(t, <-errCh, nil)

		// Проверяем, что данные были сохранены
		be.Equal(t, bs.count, 1)
	})
}

func TestSaver_Save(t *testing.T) {
	t.Run("successful save", func(t *testing.T) {
		var bs testBatchSaver

		saver := newPendingSaver(bs.saveBatch, pendingSaverConfig{
			QueueSize: 100,
			Delay:     100 * time.Millisecond,
		})
		defer saver.Close()

		// Даем время горутине запуститься
		time.Sleep(5 * time.Millisecond)

		// Сохраняем данные
		links := []model.Link{{Name: "Test", URL: "https://example.com"}}

		_, err := saver.Save(context.Background(), links)
		be.Err(t, err, nil)

		// Ждем обработки
		time.Sleep(110 * time.Millisecond)

		// Проверяем, что данные были сохранены
		be.Equal(t, bs.count, 1)
	})

	t.Run("save with context cancellation before send", func(t *testing.T) {
		var bs testBatchSaver

		saver := newPendingSaver(bs.saveBatch, pendingSaverConfig{})
		defer saver.Close()

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
		var bs testBatchSaver

		saver := newPendingSaver(bs.saveBatch, pendingSaverConfig{
			Delay: 500 * time.Millisecond, // Долгий таймаут
		})
		defer saver.Close()

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
		var bs testBatchSaver

		saver := newPendingSaver(bs.saveBatch, pendingSaverConfig{})

		// Закрываем сразу
		saver.Close()

		ctx := context.Background()
		links := []model.Link{{Name: "Test", URL: "https://example.com"}}
		_, err := saver.Save(ctx, links)
		be.Err(t, err, ErrSaverClosed)
	})

	t.Run("batch save on queue size limit", func(t *testing.T) {
		var bs testBatchSaver

		// Маленький размер очереди для теста
		saver := newPendingSaver(bs.saveBatch, pendingSaverConfig{
			QueueSize: 3,
			Delay:     500 * time.Millisecond, // Долгий таймаут
		})
		defer saver.Close()

		// Даем время горутине запуститься
		time.Sleep(5 * time.Millisecond)

		// Отправляем 3 запроса - должен сработать лимит очереди
		ctx := context.Background()
		links := []model.Link{{Name: "Test", URL: "https://example.com"}}

		for i := 0; i < 3; i++ {
			go func() {
				saver.Save(ctx, links)
			}()
		}

		// Ждем обработки
		time.Sleep(100 * time.Millisecond)

		be.Equal(t, bs.count, 3)
	})

	t.Run("batch save on timeout", func(t *testing.T) {
		var bs testBatchSaver

		// Короткий таймаут для теста
		saver := newPendingSaver(bs.saveBatch, pendingSaverConfig{
			Delay: 20 * time.Millisecond,
		})
		defer saver.Close()

		// Даем время горутине запуститься
		time.Sleep(5 * time.Millisecond)

		// Отправляем один запрос
		ctx := context.Background()
		links := []model.Link{{Name: "Test", URL: "https://example.com"}}

		go func() {
			saver.Save(ctx, links)
		}()

		// Ждем срабатывания таймаута
		time.Sleep(30 * time.Millisecond)

		// Должен быть вызов Update
		be.Equal(t, bs.count, 1)
	})

	t.Run("database error propagates", func(t *testing.T) {
		saveBatch := func(ctx context.Context, batch [][]model.Link) ([]uint64, error) {
			return nil, errors.New("save batch error")
		}

		saver := newPendingSaver(saveBatch, pendingSaverConfig{
			Delay: 10 * time.Millisecond,
		})
		defer saver.Close()

		// Даем время горутине запуститься
		time.Sleep(5 * time.Millisecond)

		ctx := context.Background()
		links := []model.Link{{Name: "Test", URL: "https://example.com"}}

		_, err := saver.Save(ctx, links)
		be.Err(t, err, "save batch error")
	})
}

func TestSaver_ServeQueue(t *testing.T) {
	t.Run("multiple concurrent saves", func(t *testing.T) {
		var bs testBatchSaver

		saver := newPendingSaver(bs.saveBatch, pendingSaverConfig{
			QueueSize: 10,
			Delay:     100 * time.Millisecond,
		})
		defer saver.Close()

		// Даем время горутине запуститься
		time.Sleep(5 * time.Millisecond)

		// Запускаем несколько горутин с сохранением
		const numSaves = 5
		results := make(chan uint64, numSaves)
		errors := make(chan error, numSaves)

		for i := 0; i < numSaves; i++ {
			go func(idx int) {
				ctx := context.Background()
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
		be.Equal(t, bs.count, numSaves)
	})

	t.Run("empty queue does nothing", func(t *testing.T) {
		var bs testBatchSaver

		saver := newPendingSaver(bs.saveBatch, pendingSaverConfig{
			Delay: 10 * time.Millisecond,
		})
		defer saver.Close()

		// Даем время горутине запуститься
		time.Sleep(20 * time.Millisecond)

		// Таймер сработает, но очередь пуста - update не должен вызываться
		be.Equal(t, bs.count, 0)
	})
}
