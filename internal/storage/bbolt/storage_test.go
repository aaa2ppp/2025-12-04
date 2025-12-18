package bbolt

import (
	"context"
	"link-checker/internal/model"
	"path/filepath"
	"sync"
	"testing"

	"github.com/aaa2ppp/be"
)

type fakeBucket struct {
	mu   sync.Mutex
	data map[string][]byte
}

// Get implements [Bucket].
func (b *fakeBucket) Get(key []byte) []byte {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.data[string(key)]
}

// Put implements [Bucket].
func (b *fakeBucket) Put(key []byte, data []byte) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.data[string(key)] = data
	return nil
}

var _ Bucket = &fakeBucket{}

type fakeTx struct {
	bucket *fakeBucket
}

// Bucket implements [Tx].
func (tx *fakeTx) Bucket(name []byte) Bucket {
	return tx.bucket
}

var _ Tx = &fakeTx{}

type fakeDB struct {
	tx *fakeTx
}

// Close implements [DB].
func (f *fakeDB) Close() error {
	panic("unimplemented")
}

// Sync implements [DB].
func (f *fakeDB) Sync() error {
	panic("unimplemented")
}

// Update implements [DB].
func (f *fakeDB) Update(fn func(tx Tx) error) error {
	return fn(f.tx)
}

// View implements [DB].
func (f *fakeDB) View(fn func(tx Tx) error) error {
	return fn(f.tx)
}

var _ DB = &fakeDB{}

func newFakeDB() *fakeDB {
	data := map[string][]byte{}
	bucket := &fakeBucket{data: data}
	tx := &fakeTx{bucket: bucket}
	return &fakeDB{tx: tx}
}

func TestStorage_saveLoad(t *testing.T) {
	db := newFakeDB()
	stor := &Storage{db: db}

	links := []model.Link{
		{Name: "Google", URL: "https://google.com"},
		{Name: "GitHub", URL: "https://github.com"},
	}

	id, err := stor.saveBatch(context.Background(), [][]model.Link{links})
	be.Err(t, err, nil)

	be.True(be.Require(t), len(db.tx.bucket.data) != 0)

	batch, err := stor.loadBatch(context.Background(), id)
	be.Err(t, err, nil)

	be.True(be.Require(t), batch.Found[0])
	be.Equal(t, batch.LinkSets[0].ID, id[0])
	be.Equal(t, batch.LinkSets[0].Links, links)
}

func TestStorage_uniqueIDs(t *testing.T) {
	db := newFakeDB()
	stor := &Storage{db: db}

	links := []model.Link{
		{Name: "Google", URL: "https://google.com"},
		{Name: "GitHub", URL: "https://github.com"},
	}

	const N = 100000

	ids := make(map[uint64]struct{}, N)
	for i := 0; i < N; i++ {
		id, err := stor.saveBatch(context.Background(), [][]model.Link{links})
		be.Err(t, err, nil)
		ids[id[0]] = struct{}{}
	}

	be.Equal(be.Require(t), len(ids), N)

	found := 0
	for id := range ids {
		batch, err := stor.loadBatch(context.Background(), []uint64{id})
		be.Err(t, err, nil)
		if batch.Found[0] {
			found++
		}
	}

	be.Equal(t, found, N)
}

func TestStorage_cacheDataSafety(t *testing.T) {
	db := newFakeDB()
	stor := &Storage{db: db, cache: newCache(100)}

	ctx := context.Background()

	// 1. Внешнее изменение после Save
	externalLinks := []model.Link{
		{Name: "Original", URL: "https://example.com"},
	}

	id, err := stor.Save(ctx, externalLinks)
	be.Err(t, err, nil)

	// Меняем оригинал
	externalLinks[0].Name = "ModifiedAfterSave"

	// Загружаем - должно быть оригинальное
	linkSet, err := stor.Load(ctx, id)
	be.Err(t, err, nil)
	be.Equal(t, linkSet.Links[0].Name, "Original")

	// 2. Изменение после Load
	linkSet.Links[0].Name = "ModifiedAfterLoad"

	// Снова загружаем
	linkSet2, err := stor.Load(ctx, id)
	be.Err(t, err, nil)
	be.Equal(t, linkSet2.Links[0].Name, "Original")
}

func TestStorage_SaveLoad(t *testing.T) {
	tmpFile := filepath.Join(t.TempDir(), "test.db")

	ctx := context.Background()

	cfg := Config{
		CacheSize: 100,
		DataFile:  tmpFile,
		NoSync:    true,
		Logger:    newTestLogger(),
	}

	links := []model.Link{
		{Name: "Google", URL: "https://google.com"},
		{Name: "GitHub", URL: "https://github.com"},
	}

	// 1. Создание новой записи. В отдельной функции, чтобы defer на закрыие отработал
	id := func() uint64 {
		storage1, err := Open(cfg)
		be.Err(t, err, nil)
		defer func() {
			err := storage1.Close(ctx)
			be.Err(t, err, nil)
		}()

		// Сохраняем
		id, err := storage1.Save(ctx, links)
		be.Err(t, err, nil)

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

		return id
	}()

	// 2. Открывает заново, читаем запись сохраненную в прошлом сеанса
	func(id uint64) {
		storage2, err := Open(cfg)
		be.Err(t, err, nil)
		defer func() {
			err = storage2.Close(ctx)
			be.Err(t, err, nil)
		}()

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
	}(id)
}

// Тест, чтобы запустить в дебаг режиме и убедится, что fdatasync гарантированно выполняется
func TestStorage_Close(t *testing.T) {
	tmpFile := filepath.Join(t.TempDir(), "test.db")

	storage, err := Open(Config{
		DataFile: tmpFile,
		NoSync:   true,
		Logger:   newTestLogger(),
	})
	be.Err(t, err, nil)

	storage.Close(context.Background())
}
