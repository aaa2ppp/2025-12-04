// Пакет bbolt предоставляет реализацию хранилища на основе BoltDB.
//
// Примечание по отмене контекста:
//
// Вызывающий код должен использовать тайм-ауты контекста для управления продолжительностью операции.
// Хотя методы должным образом проверяют отмену контекста и возвращают ошибки при завершении контекста,
// базовые транзакции BoltDB могут продолжать выполняться в фоновом режиме из-за архитектуры BoltDB.
package bbolt

import (
	"cmp"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"os"
	"path/filepath"
	"time"

	"go.etcd.io/bbolt"

	"link-checker/internal/logger"
	"link-checker/internal/model"
	"link-checker/internal/service"
)

const (
	DefaultMaxBatchSize = 2048
)

var linkSetBucket = []byte("linksets")

type Config struct {
	// DataFile - путь к файлу базы данных (required)
	DataFile string

	// OpenTimeout - максимальное время ожидания файловой блокировки при открытии базы.
	// Передается в bbolt.Open.
	// 0 - ждать бесконечно (по умолчанию).
	// Не влияет на время выполнения операций чтения/записи.
	OpenTimeout time.Duration

	// Logger используется для логирования операций базы данных.
	// Приоритеты использования логгера:
	// 1. Логгер из контекста (если метод принимает context.Context и в нем установлен логгер)
	// 2. Этот логгер (если не nil)
	// 3. slog.Default() (глобальный логгер по умолчанию)
	Logger *slog.Logger

	// NoSync - отключает немедленную синхронизацию с диском.
	// Игнорируется, если задан MaxSyncDelay или MaxSyncPending.
	// В этом случае bbolt.Open получает NoSync=true, а управление
	// синхронизацией осуществляется через MaxSyncDelay/MaxSyncPending.
	NoSync bool

	// READ CACHE

	// CacheSize - максимальное количество записей в LRU кеше.
	// Значение <= 0 отключает кеш.
	CacheSize int

	// DISK SYNC (отложенная синхронизация)

	// MaxSyncDelay - если > 0, задает максимальное время,
	// в течение которого запись может оставаться не синхронизированной с диском.
	// После истечения этого времени все накопленные изменения сбрасываются на диск.
	// Если заданы и MaxSyncDelay, и MaxSyncPending - срабатывает первое ограничение.
	MaxSyncDelay time.Duration

	// MaxSyncPending - если > 0, задает максимальное количество операций записи,
	// которые могут накопиться без синхронизации. При достижении этого порога
	// выполняется принудительная синхронизация.
	// Для Save каждая операция считается отдельно, для SaveBatch - размер пакета.
	// Если заданы и MaxSyncDelay, и MaxSyncPending - срабатывает первое ограничение.
	MaxSyncPending int

	// WRITE BATCHING (группировка операций)

	// MaxBatchDelay - если > 0, одиночные операции Save объединяются в пакеты.
	// Определяет максимальное время ожидания перед выполнением собранного пакета.
	// Если MaxBatchDelay > 0, а MaxBatchSize <= 0, используется DefaultMaxBatchSize.
	// NOTE: Если между вызовами Save происходит явный вызов SaveBatch,
	// он выполняется немедленно и не объединяется с накапливаемым пакетом.
	// Это поведение может измениться в будущем.
	MaxBatchDelay time.Duration

	// MaxBatchSize - если > 0, задает максимальный размер пакета операций.
	// При достижении этого размера пакет выполняется немедленно.
	// Если задан MaxBatchDelay, но MaxBatchSize <= 0,
	// используется значение DefaultMaxBatchSize.
	MaxBatchSize int
}

type Storage struct {
	db         DB
	cache      *cache
	logger     *slog.Logger
	lazySyncer *lazySyncer
	batchSaver *batchSaver
}

// Open открывает базу данных с заданной конфигурацией.
func Open(cfg Config) (*Storage, error) {
	logger := cmp.Or(cfg.Logger, slog.Default())

	if cfg.DataFile == "" {
		return nil, errors.New("DataFile is required")
	}

	dir := filepath.Dir(cfg.DataFile)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("create data dir: %w", err)
	}

	bops := &bbolt.Options{
		Timeout: max(cfg.OpenTimeout, 0),
		NoSync:  cfg.NoSync || cfg.MaxSyncDelay > 0 || cfg.MaxSyncPending > 0,
	}

	logger.Info("open database", "data_file", cfg.DataFile, "open_timeout", bops.Timeout, "sync_mode", syncModeName(cfg))

	db, err := bbolt.Open(cfg.DataFile, 0600, bops)
	if err != nil {
		return nil, fmt.Errorf("open bolt db: %w", err)
	}

	err = db.Update(func(tx *bbolt.Tx) error {
		_, err := tx.CreateBucketIfNotExists(linkSetBucket)
		return err
	})
	if err != nil {
		db.Close()
		return nil, fmt.Errorf("init bucket: %w", err)
	}

	var cache *cache
	if cfg.CacheSize > 0 {
		cache = newCache(cfg.CacheSize)
	}

	storage := &Storage{
		db:     bboltDB{db},
		cache:  cache,
		logger: logger,
	}

	var lazySyncer *lazySyncer
	if cfg.MaxSyncDelay > 0 || cfg.MaxSyncPending > 0 {
		lazySyncer = newLazySyncer(
			storage.sync,
			cfg.MaxSyncPending,
			cfg.MaxSyncDelay,
		)
		storage.lazySyncer = lazySyncer
	}

	if cfg.MaxBatchDelay > 0 && cfg.MaxBatchSize <= 0 {
		logger.Warn("MaxBatchDelay set but MaxBatchSize not set, using default", "default", DefaultMaxBatchSize)
	}

	var batchSaver *batchSaver
	if cfg.MaxBatchDelay > 0 || cfg.MaxBatchSize > 0 {
		batchSize := cfg.MaxBatchSize
		if batchSize <= 0 {
			batchSize = DefaultMaxBatchSize
		}
		batchSaver = newBatchSaver(
			func(batch [][]model.Link) ([]uint64, error) {
				return storage.saveBatch(context.Background(), batch)
			},
			batchSize,
			cfg.MaxBatchDelay,
		)
		storage.batchSaver = batchSaver
	}

	return storage, nil
}

func syncModeName(cfg Config) string {
	switch {
	case cfg.MaxSyncDelay > 0 || cfg.MaxSyncPending > 0:
		return "lazy_sync"
	case cfg.NoSync:
		return "no_sync"
	default:
		return "immediate"
	}
}

func (s *Storage) Close(ctx context.Context) error {
	const op = "Storage.Close"

	if err := ctx.Err(); err != nil {
		return err
	}

	s.logger.Info("close database")

	if s.batchSaver != nil {
		if err := s.batchSaver.Close(ctx); err != nil {
			s.logger.Warn("close batch saver failed", "op", op, "error", err)
		}
	}

	if s.lazySyncer != nil {
		s.lazySyncer.Close()
	}

	var (
		done = make(chan struct{}, 1)
		err  error
	)
	go func() {
		defer close(done)
		err = s.db.Close()
	}()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-done:
	}

	if err != nil {
		s.logger.Error("database close failed", "op", op, "error", err)
		return model.ErrInternalError
	}

	return nil
}

func (s *Storage) Save(ctx context.Context, links []model.Link) (uint64, error) {
	if s.batchSaver != nil {
		return s.batchSaver.Save(ctx, links)
	}

	resp, err := s.SaveBatch(ctx, [][]model.Link{links})
	if err != nil {
		return 0, err
	}
	return resp[0], nil
}

func (s *Storage) SaveBatch(ctx context.Context, links [][]model.Link) ([]uint64, error) {
	if len(links) == 0 {
		return nil, nil
	}

	if err := ctx.Err(); err != nil {
		return nil, err
	}

	// BBolt не поддерживает контекст - выполняем транзакцию в отдельной горутине проверяя контекст
	var (
		ids  []uint64
		err  error
		done = make(chan struct{}, 1)
	)
	go func() {
		defer close(done)
		ids, err = s.saveBatch(ctx, links)
	}()

	// Ждем завершения транзакции или отмены контекста
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-done:
	}

	if err != nil {
		return nil, err
	}

	return ids, nil
}

func (s *Storage) Load(ctx context.Context, id uint64) (model.LinkSet, error) {
	resp, err := s.LoadBatch(ctx, []uint64{id})
	if err != nil {
		return model.LinkSet{}, err
	}
	if !resp.Found[0] {
		return model.LinkSet{}, model.ErrNotFound
	}
	return resp.LinkSets[0], nil
}

func (s *Storage) LoadBatch(ctx context.Context, ids []uint64) (LinkSetBatch, error) {
	if err := ctx.Err(); err != nil {
		return LinkSetBatch{}, err
	}

	var (
		batch LinkSetBatch
		err   error
		done  = make(chan struct{}, 1)
	)
	go func() {
		defer close(done)
		batch, err = s.loadBatch(ctx, ids)
	}()

	select {
	case <-ctx.Done():
		return LinkSetBatch{}, ctx.Err()
	case <-done:
	}

	if err != nil {
		return LinkSetBatch{}, err
	}

	return batch, nil
}

func makeKey(id uint64) []byte {
	key := make([]byte, 8)
	binary.BigEndian.PutUint64(key, id)
	return key
}

func generateID(b Bucket) (uint64, []byte, error) {
	for range 10 {
		id := rand.Uint64()
		if id == 0 {
			continue
		}

		key := makeKey(id)
		if b.Get(key) != nil {
			continue
		}

		return id, key, nil
	}

	return 0, nil, errors.New("too many ID collisions")
}

func (s *Storage) saveBatch(ctx context.Context, links [][]model.Link) ([]uint64, error) {
	const op = "Storage.saveBatch"

	if len(links) == 0 {
		return nil, nil
	}

	// Сюда соберем сгенерированные IDs для новых записей
	ids := make([]uint64, len(links))

	// Сохраняем пачку запрос в базу одной транзакцией
	err := s.db.Update(func(tx Tx) error {
		b := tx.Bucket(linkSetBucket)

		for i := range links {
			if err := ctx.Err(); err != nil {
				return err
			}

			id, key, err := generateID(b)
			if err != nil {
				logger.FromContextDef(ctx, s.logger).Error("generate id failed", "op", op, "error", err)
				return model.ErrInternalError
			}

			ids[i] = id

			linkSet := model.LinkSet{
				ID:    id,
				Links: links[i],
			}

			data, err := json.Marshal(linkSet)
			if err != nil {
				logger.FromContextDef(ctx, s.logger).Error("marshal linkset failed", "op", op, "error", err)
				return model.ErrInternalError
			}

			if err := b.Put(key, data); err != nil {
				logger.FromContextDef(ctx, s.logger).Error("database write failed", "op", op, "error", err)
				return model.ErrInternalError
			}
		}

		return nil
	})

	if err != nil {
		if err != model.ErrInternalError {
			logger.FromContextDef(ctx, s.logger).Error("database update failed", "op", op, "error", err)
		}
		return nil, model.ErrInternalError
	}

	if s.lazySyncer != nil {
		s.lazySyncer.Update(len(ids))
	}

	if s.cache != nil {
		s.cache.Do(func(c lockedCache) {
			for i := range links {
				linkSet := &model.LinkSet{
					ID:    ids[i],
					Links: model.CloneLinks(links[i]),
				}
				c.Put(linkSet)
			}
		})
	}

	return ids, nil
}

type LinkSetBatch = service.LinkSetBatch

func (s *Storage) loadBatch(ctx context.Context, ids []uint64) (LinkSetBatch, error) {
	const op = "Storage.loadBatch"

	linkSets := make([]model.LinkSet, len(ids))
	found := make([]bool, len(ids))

	if s.cache != nil {
		count := 0
		s.cache.Do(func(lc lockedCache) {
			for i, id := range ids {
				if linkSet := lc.Get(id); linkSet != nil {
					linkSets[i] = linkSet.Clone()
					found[i] = true
					count++
				}
			}
		})
		if count == len(ids) {
			return LinkSetBatch{LinkSets: linkSets, Found: found}, nil
		}
	}

	var loaded []int
	err := s.db.View(func(tx Tx) error {
		b := tx.Bucket(linkSetBucket)

		for i, id := range ids {
			if found[i] {
				continue
			}

			if err := ctx.Err(); err != nil {
				return err
			}

			key := makeKey(id)
			data := b.Get(key)
			if data == nil {
				continue
			}

			if err := json.Unmarshal(data, &linkSets[i]); err != nil {
				logger.FromContextDef(ctx, s.logger).Error("unmarshal linkset failed", "op", op, "error", err, "link_set_id", id)
				return model.ErrInternalError
			}

			found[i] = true

			if s.cache != nil {
				loaded = append(loaded, i)
			}
		}
		return nil
	})

	if err != nil {
		if err != model.ErrInternalError {
			logger.FromContextDef(ctx, s.logger).Error("database update failed", "op", op, "error", err)
		}
		return service.LinkSetBatch{}, model.ErrInternalError
	}

	if s.cache != nil {
		s.cache.Do(func(lc lockedCache) {
			for _, i := range loaded {
				cloned := linkSets[i].Clone()
				lc.Put(&cloned)
			}
		})
	}

	return LinkSetBatch{LinkSets: linkSets, Found: found}, nil
}

func (s *Storage) sync() error {
	const op = "Storage.sync"

	if err := s.db.Sync(); err != nil {
		s.logger.Error("database sync failed", "op", op, "error", err)
		return model.ErrInternalError
	}
	return nil
}

var _ service.LinkStorage = &Storage{}
