package bbolt

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"link-checker/internal/logger"
	"link-checker/internal/model"

	"github.com/aaa2ppp/be"
)

func newTestLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
}

func TestStorage_SaveLoad(t *testing.T) {
	storLogger := newTestLogger()
	tmpFile := filepath.Join(t.TempDir(), "test.db")
	defer os.Remove(tmpFile)

	ctx := logger.Context(context.Background(), storLogger)

	storage1, err := Open(Config{MaxCache: 100, DataFile: tmpFile, Logger: storLogger})
	be.Err(t, err, nil)

	links := []model.Link{
		{Name: "Google", URL: "https://google.com"},
		{Name: "GitHub", URL: "https://github.com"},
	}

	// Сохраняем
	id, err := storage1.Save(ctx, links)
	be.Err(t, err, nil)
	defer storage1.Close()

	{
		// Проверяем, что попало в кеш
		linkSetPtr := storage1.cache.Get(id)
		be.True(t, linkSetPtr != nil)
		if linkSetPtr != nil {
			be.Equal(t, linkSetPtr.ID, id)
			be.Equal(t, linkSetPtr.Links, links)
		}

		// Загружаем (сейчас должен взять из кеша)
		linkSet, err := storage1.Load(ctx, id)
		be.Err(t, err, nil)
		be.Equal(t, linkSet.ID, id)
		be.Equal(t, linkSet.Links, links)
	}

	err = storage1.Close()
	be.Err(t, err, nil)

	storage2, err := Open(Config{MaxCache: 100, DataFile: tmpFile, Logger: storLogger})
	be.Err(t, err, nil)
	defer storage2.Close()

	{
		// Загружаем (сейчас кеш пустой - должен взять из файла)
		linkSet, err := storage2.Load(ctx, id)
		be.Err(t, err, nil)
		be.Equal(t, linkSet.ID, id)
		be.Equal(t, linkSet.Links, links)

		// Проверяем, что попало в кеш
		linkSetPtr := storage2.cache.Get(id)
		be.True(t, linkSetPtr != nil)
		if linkSetPtr != nil {
			be.Equal(t, linkSetPtr.ID, id)
			be.Equal(t, linkSetPtr.Links, links)
		}
	}

	err = storage2.Close()
	be.Err(t, err, nil)
}

func TestStorage_DataSafety(t *testing.T) {
	storLogger := newTestLogger()
	tmpFile := filepath.Join(t.TempDir(), "test.db")
	defer os.Remove(tmpFile)

	ctx := logger.Context(context.Background(), storLogger)
	storage, err := Open(Config{MaxCache: 100, DataFile: tmpFile, Logger: storLogger})
	be.Err(t, err, nil)
	defer storage.Close()

	// 1. Внешнее изменение после Save
	externalLinks := []model.Link{
		{Name: "Original", URL: "https://example.com"},
	}

	id, err := storage.Save(ctx, externalLinks)
	be.Err(t, err, nil)

	// Меняем оригинал
	externalLinks[0].Name = "ModifiedAfterSave"

	// Загружаем - должно быть оригинальное
	linkSet, err := storage.Load(ctx, id)
	be.Err(t, err, nil)
	be.Equal(t, linkSet.Links[0].Name, "Original")

	// 2. Изменение после Load
	linkSet.Links[0].Name = "ModifiedAfterLoad"

	// Снова загружаем
	linkSet2, err := storage.Load(ctx, id)
	be.Err(t, err, nil)
	be.Equal(t, linkSet2.Links[0].Name, "Original")
}

func TestStorage_UniqueIDs(t *testing.T) {
	storLogger := newTestLogger()
	tmpFile := filepath.Join(t.TempDir(), "test.db")
	defer os.Remove(tmpFile)

	ctx := logger.Context(context.Background(), storLogger)
	storage, err := Open(Config{MaxCache: 100, DataFile: tmpFile, Logger: storLogger})
	be.Err(t, err, nil)
	defer storage.Close()

	N := 1000
	links := []model.Link{{Name: "Test", URL: "https://test.com"}}

	var idsMu sync.Mutex
	ids := make(map[uint64]struct{}, N)

	var wg sync.WaitGroup

	wg.Add(N)
	for range N {
		go func() {
			defer wg.Done()
			id, _ := storage.Save(ctx, links)
			idsMu.Lock()
			defer idsMu.Unlock()
			ids[id] = struct{}{}
		}()
	}
	wg.Wait()

	// Проверяем, что нет дублей ID
	be.Equal(t, len(ids), N)

	// Проверяем, что все ID читаются
	for id := range ids {
		linkSet, err := storage.Load(ctx, id)
		be.Err(t, err, nil)
		be.Equal(be.Require(t), linkSet.Links, links)
	}
}

// Тест, чтобы запустить в дебаг режиме и убедится, что fdatasync гарантированно выполняется
func TestStorage_Close(t *testing.T) {
	storLogger := newTestLogger()
	tmpFile := filepath.Join(t.TempDir(), "test.db")
	defer os.Remove(tmpFile)

	storage, err := Open(Config{MaxCache: 100, DataFile: tmpFile, Logger: storLogger})
	be.Err(t, err, nil)
	defer storage.Close()
}
