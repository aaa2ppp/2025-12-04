package storage

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

type NoSyncMode int

const (
	NoSyncUndefined NoSyncMode = iota
	NoSyncEnabled
	NoSyncDisabled
)

const (
	DefaultMaxCache = 1000
	DefaultDataFile = "./data/linksets.db"
	DefaultTimeout  = 1 * time.Second
	DefaultNoSync   = true
)

var LinkSetBucket = []byte("linksets")

type Config struct {
	DataFile   string
	MaxCache   int
	Timeout    time.Duration
	NoSyncMode NoSyncMode
}

type Storage struct {
	db    *bbolt.DB
	cache *cache
}

func Open(cfg Config) (*Storage, error) {
	maxCache := cmp.Or(cfg.MaxCache, DefaultMaxCache)
	cache := newCache(maxCache)

	dataFile := cmp.Or(cfg.DataFile, DefaultDataFile)
	dir := filepath.Dir(dataFile)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("create data dir: %w", err)
	}

	var noSync bool
	switch cfg.NoSyncMode {
	case NoSyncUndefined:
		noSync = DefaultNoSync
	case NoSyncEnabled:
		noSync = true
	}

	timeout := cmp.Or(cfg.Timeout, DefaultTimeout)
	if timeout < 0 {
		timeout = 0
	}

	db, err := bbolt.Open(dataFile, 0600, &bbolt.Options{
		Timeout: timeout,
		NoSync:  noSync,
	})
	if err != nil {
		return nil, fmt.Errorf("open bolt db: %w", err)
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
		db:    db,
		cache: cache,
	}, nil
}

func makeKey(id uint64) []byte {
	key := make([]byte, 8)
	binary.BigEndian.PutUint64(key, id)
	return key
}

func (s *Storage) generateID(b *bbolt.Bucket) (uint64, []byte, error) {
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

	return 0, nil, fmt.Errorf("too many ID collisions (10 attempts)")
}

func (s *Storage) Save(ctx context.Context, links []model.Link) (uint64, error) {
	const op = "Storage.Save"

	linkSet := model.LinkSet{
		Links: model.CloneLinks(links),
	}

	err := s.db.Update(func(tx *bbolt.Tx) error {
		b := tx.Bucket(LinkSetBucket)

		id, key, err := s.generateID(b)
		if err != nil {
			logger.FromContext(ctx).Error("generate id", "error", err, "op", op)
			return model.ErrInternalError
		}

		linkSet.ID = id

		data, err := json.Marshal(linkSet)
		if err != nil {
			logger.FromContext(ctx).Error("marshal linkset", "error", err, "op", op)
			return model.ErrInternalError
		}

		if err := b.Put(key, data); err != nil {
			logger.FromContext(ctx).Error("database write", "error", err, "op", op)
			return model.ErrInternalError
		}

		return nil
	})

	if err != nil {
		return 0, err
	}

	s.cache.set(linkSet.ID, &linkSet)

	return linkSet.ID, nil
}

func (s *Storage) Load(ctx context.Context, id uint64) (model.LinkSet, error) {
	const op = "Storage.Load"

	if linkSet, ok := s.cache.get(id); ok {
		return linkSet.Clone(), nil
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
		logger.FromContext(ctx).Error("database read", "error", err, "op", op, "link_set_id", id)
		return model.LinkSet{}, model.ErrInternalError
	}

	var linkSet model.LinkSet
	if err := json.Unmarshal(data, &linkSet); err != nil {
		logger.FromContext(ctx).Error("unmarshal linkset", "error", err, "op", op, "link_set_id", id)
		return model.LinkSet{}, model.ErrInternalError
	}

	s.cache.set(id, &linkSet)

	return linkSet.Clone(), nil
}

func (s *Storage) Close() error {

	// Гарантируем, что после выхода из Close все изменения будут сохранены на диске.
	// После сброса флага NoSync последующие транзакции записи будут выполняться fdatasync.
	s.db.NoSync = false

	// Гарантируем, что после сброса флага NoSync будет по крайней мере одна транзакция записи,
	// котрая выполнит fdatasync.
	err := s.db.Update(func(tx *bbolt.Tx) error {
		return nil
	})
	if err != nil && err != berrors.ErrDatabaseNotOpen {
		slog.Warn("storage: final sync transaction failed", "error", err)
	}

	// db.Close ожидает завершения всех незавершенных транзакций (если такие есть).
	// В bbolt одновременно может существовать только одна транзакция записи.
	// Это означает, что транзакции записи, которые ожидает db.Close, выполняются после
	// нашей пустой транзакции и, следовательно, после сброса флага NoSync и будут
	// выполнять fdatasync.
	return s.db.Close()
}

var _ service.LinkStorage = &Storage{}
