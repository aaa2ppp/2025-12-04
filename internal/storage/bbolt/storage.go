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
	berrors "go.etcd.io/bbolt/errors"

	"link-checker/internal/logger"
	"link-checker/internal/model"
	"link-checker/internal/service"
)

const (
	DefaultMaxCache = 10000
	DefaultDataFile = "./data/linksets.db"
	DefaultTimeout  = 1 * time.Second
	DefaultNoSync   = true
)

var ErrTooManyIDCollisions = errors.New("too many ID collisions")

var LinkSetBucket = []byte("linksets")

type Config struct {
	DataFile string
	MaxCache int
	Timeout  time.Duration
	NoSync   bool
	Logger   *slog.Logger

	MaxNotSyncedUpdates int
	MaxSyncDelay        time.Duration

	MaxSaveQueueSize int
	MaxSaveDelay     time.Duration
}

type Storage struct {
	db     *bbolt.DB
	cache  *cache
	syncer *syncer
	logger *slog.Logger
	saver  *saver
}

func Open(cfg Config) (*Storage, error) {
	maxCache := cmp.Or(cfg.MaxCache, DefaultMaxCache)
	cache := newCache(maxCache)

	logger := cmp.Or(cfg.Logger, slog.Default())

	dataFile := cmp.Or(cfg.DataFile, DefaultDataFile)

	dir := filepath.Dir(dataFile)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("create data dir: %w", err)
	}

	bops := &bbolt.Options{
		Timeout: max(cmp.Or(cfg.Timeout, DefaultTimeout), 0),
		NoSync:  cfg.NoSync,
	}

	logger.Info("open database", "data_file", dataFile, "timeout", bops.Timeout, "no_sync", bops.NoSync,
		"max_cache", cache.maxSize)

	db, err := bbolt.Open(dataFile, 0600, bops)
	if err != nil {
		return nil, fmt.Errorf("open bolt db: %w", err)
	}

	var syncer *syncer
	if cfg.NoSync && cfg.MaxNotSyncedUpdates >= 0 && cfg.MaxSyncDelay >= 0 {
		syncer = newSyncer(db, logger, syncerConfig{
			maxNotSyncedUpdates: cfg.MaxNotSyncedUpdates,
			maxSyncDelay:        cfg.MaxSyncDelay,
		})
	}

	var saver *saver
	if cfg.MaxSaveQueueSize >= 0 && cfg.MaxSaveDelay >= 0 {
		saver = newSaver(db, logger, saverConfig{
			cache:        cache,
			syncer:       syncer,
			maxQueueSize: cfg.MaxSaveQueueSize,
			maxDelay:     cfg.MaxSaveDelay,
		})
	}

	err = db.Update(func(tx *bbolt.Tx) error {
		_, err := tx.CreateBucketIfNotExists(LinkSetBucket)
		return err
	})
	if err != nil {
		db.Close()
		return nil, fmt.Errorf("init bucket: %w", err)
	}

	return &Storage{
		db:     db,
		cache:  cache,
		syncer: syncer,
		saver:  saver,
		logger: logger,
	}, nil
}

func makeKey(id uint64) []byte {
	key := make([]byte, 8)
	binary.BigEndian.PutUint64(key, id)
	return key
}

func generateID(b *bbolt.Bucket) (uint64, []byte, error) {
	for range GenerateIDAttempts {
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

	return 0, nil, ErrTooManyIDCollisions
}

func (s *Storage) Save(ctx context.Context, links []model.Link) (uint64, error) {
	const op = "Storage.Save"

	if s.saver != nil {
		return s.saver.Save(ctx, links)
	}

	linkSet := model.LinkSet{
		Links: model.CloneLinks(links),
	}

	err := s.db.Update(func(tx *bbolt.Tx) error {
		b := tx.Bucket(LinkSetBucket)

		id, key, err := generateID(b)
		if err != nil {
			logger.FromContextDef(ctx, s.logger).Error("generate id failed", "op", op, "error", err)
			return model.ErrInternalError
		}

		linkSet.ID = id

		data, err := json.Marshal(linkSet)
		if err != nil {
			logger.FromContextDef(ctx, s.logger).Error("marshal linkset failed", "op", op, "error", err)
			return model.ErrInternalError
		}

		if err := b.Put(key, data); err != nil {
			logger.FromContextDef(ctx, s.logger).Error("database write failed", "op", op, "error", err)
			return model.ErrInternalError
		}

		return nil
	})

	if err != nil {
		return 0, err
	}

	if s.syncer != nil {
		s.syncer.Update()
	}

	s.cache.set(linkSet.ID, &linkSet)

	return linkSet.ID, nil
}

func (s *Storage) Load(ctx context.Context, id uint64) (model.LinkSet, error) {
	const op = "Storage.Load"

	if linkSet := s.cache.Get(id); linkSet != nil {
		return linkSet.Clone(), nil
	}

	if err := ctx.Err(); err != nil {
		return model.LinkSet{}, err
	}

	var data []byte
	err := s.db.View(func(tx *bbolt.Tx) error {
		b := tx.Bucket(LinkSetBucket)

		key := makeKey(id)
		data = b.Get(key)
		if data == nil {
			return fmt.Errorf("linkset %d: %w", id, model.ErrNotFound)
		}
		return nil
	})
	if err != nil {
		if errors.Is(err, model.ErrNotFound) {
			return model.LinkSet{}, err
		}
		logger.FromContextDef(ctx, s.logger).Error("database read failed", "op", op, "error", err, "link_set_id", id)
		return model.LinkSet{}, model.ErrInternalError
	}

	var linkSet model.LinkSet
	if err := json.Unmarshal(data, &linkSet); err != nil {
		logger.FromContextDef(ctx, s.logger).Error("unmarshal linkset failed", "op", op, "error", err, "link_set_id", id)
		return model.LinkSet{}, model.ErrInternalError
	}

	s.cache.Set(&linkSet)

	return linkSet.Clone(), nil
}

type LinkSetBatch = service.LinkSetBatch

func (s *Storage) LoadBatch(ctx context.Context, ids []uint64) (LinkSetBatch, error) {
	const op = "Storage.LoadBatch"

	linkSets := make([]model.LinkSet, len(ids))
	found := make([]bool, len(ids))
	foundCount := 0

	for i, id := range ids {
		if linkSet := s.cache.Get(id); linkSet != nil {
			linkSets[i] = linkSet.Clone()
			found[i] = true
			foundCount++
		}

	}

	if foundCount == len(ids) {
		return LinkSetBatch{LinkSets: linkSets, Found: found}, nil
	}

	if err := ctx.Err(); err != nil {
		return LinkSetBatch{}, err
	}

	var data []byte

	err := s.db.View(func(tx *bbolt.Tx) error {
		b := tx.Bucket(LinkSetBucket)

		for i, id := range ids {
			if found[i] {
				continue
			}

			if err := ctx.Err(); err != nil {
				return err
			}

			key := makeKey(id)
			data = b.Get(key)
			if data == nil {
				continue
			}

			if err := json.Unmarshal(data, &linkSets[i]); err != nil {
				logger.FromContextDef(ctx, s.logger).Error("unmarshal linkset failed", "op", op, "error", err, "link_set_id", id)
				return model.ErrInternalError
			}

			found[i] = true
			foundCount++
		}

		return nil
	})
	if err != nil {
		if errors.Is(err, model.ErrInternalError) {
			return LinkSetBatch{}, err
		}
		logger.FromContextDef(ctx, s.logger).Error("database read failed", "op", op, "error", err)
		return LinkSetBatch{}, model.ErrInternalError
	}

	return LinkSetBatch{LinkSets: linkSets, Found: found}, nil
}

func (s *Storage) Close() error {
	const op = "Storage.Close"

	s.logger.Info("close database")

	if s.saver != nil {
		if err := s.saver.Close(); err != nil {
			s.logger.Warn("close saver failed", "op", op, "error", err)
		}
	}

	if s.syncer != nil {
		s.syncer.Close()
	}

	// Гарантируем, что после выхода из Close все изменения будут сохранены на диске.
	// После сброса флага NoSync последующие транзакции записи будут выполняться fdatasync.
	s.db.NoSync = false

	// Гарантируем, что после сброса флага NoSync будет по крайней мере одна транзакция записи,
	// котрая выполнит fdatasync.
	err := s.db.Update(func(tx *bbolt.Tx) error {
		return nil
	})
	if err != nil && err != berrors.ErrDatabaseNotOpen {
		s.logger.Warn("final sync transaction failed", "op", op, "error", err)
	}

	// db.Close ожидает завершения всех незавершенных транзакций (если такие есть).
	// В bbolt одновременно может существовать только одна транзакция записи.
	// Это означает, что транзакции записи, которые ожидает db.Close, выполняются после
	// нашей пустой транзакции и, следовательно, после сброса флага NoSync и будут
	// выполнять fdatasync.
	return s.db.Close()
}

var _ service.LinkStorage = &Storage{}
