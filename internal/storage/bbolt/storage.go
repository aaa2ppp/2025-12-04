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

const generateIDAttempts = 10

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

	CheckIDCollisions bool
}

type Storage struct {
	db *bbolt.DB
	*storage
	pending *pendingSaver
}

func Open(cfg Config) (*Storage, error) {
	logger := cmp.Or(cfg.Logger, slog.Default())

	var cache *cache
	if cfg.MaxCache >= 0 {
		maxCache := cmp.Or(cfg.MaxCache, DefaultMaxCache)
		cache = newCache(maxCache)
	}

	dataFile := cmp.Or(cfg.DataFile, DefaultDataFile)

	dir := filepath.Dir(dataFile)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("create data dir: %w", err)
	}

	bops := &bbolt.Options{
		Timeout: max(cmp.Or(cfg.Timeout, DefaultTimeout), 0),
		NoSync:  cfg.NoSync,
	}

	logger.Info("open database", "data_file", dataFile, "timeout", bops.Timeout, "no_sync", bops.NoSync)

	db, err := bbolt.Open(dataFile, 0600, bops)
	if err != nil {
		return nil, fmt.Errorf("open bolt db: %w", err)
	}

	var syncer *periodicSyncer
	if cfg.NoSync && cfg.MaxNotSyncedUpdates >= 0 && cfg.MaxSyncDelay >= 0 {
		syncer = newSyncer(bboltDB{db}, logger, syncerConfig{
			maxNotSyncedUpdates: cfg.MaxNotSyncedUpdates,
			maxSyncDelay:        cfg.MaxSyncDelay,
		})
	}

	storage := &storage{
		db:     bboltDB{db},
		cache:  cache,
		syncer: syncer,
		logger: logger,
	}

	var pending *pendingSaver
	if cfg.MaxSaveQueueSize >= 0 && cfg.MaxSaveDelay >= 0 {
		pending = newPendingSaver(storage.SaveBatch, pendingSaverConfig{
			QueueSize: cfg.MaxSaveQueueSize,
			Delay:     cfg.MaxSaveDelay,
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
		db:      db,
		storage: storage,
		pending: pending,
	}, nil
}

func makeKey(id uint64) []byte {
	key := make([]byte, 8)
	binary.BigEndian.PutUint64(key, id)
	return key
}

func generateID(b Bucket) (uint64, []byte, error) {
	for range generateIDAttempts {
		id := rand.Uint64()
		if id == 0 {
			continue
		}

		key := makeKey(id)
		if b != nil && b.Get(key) != nil {
			continue
		}

		return id, key, nil
	}

	return 0, nil, ErrTooManyIDCollisions
}

func (s *Storage) Save(ctx context.Context, links []model.Link) (uint64, error) {
	if s.pending != nil {
		return s.pending.Save(ctx, links)
	}
	return s.storage.Save(ctx, links)
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

	if s.db.NoSync {
		// Гарантируем, что после выхода из Close все изменения будут сохранены на диске.
		// После сброса флага NoSync последующие транзакции записи будут выполняться fdatasync.
		s.db.NoSync = false

		// Гарантируем, что после сброса флага NoSync будет по крайней мере одна транзакция записи,
		// котрая выполнит fdatasync.
		err := s.db.Update(func(tx *bbolt.Tx) error { return nil })
		if err != nil && err != berrors.ErrDatabaseNotOpen {
			s.logger.Warn("final sync transaction failed", "op", op, "error", err)
		}
	}

	// db.Close ожидает завершения всех незавершенных транзакций (если такие есть).
	// В bbolt одновременно может существовать только одна транзакция записи.
	// Это означает, что транзакции записи, которые ожидает db.Close, выполняются после
	// нашей пустой транзакции и, следовательно, после сброса флага NoSync и будут
	// выполнять fdatasync.
	return s.db.Close()
}

type storage struct {
	db     DB
	cache  *cache
	syncer *periodicSyncer
	saver  *pendingSaver
	logger *slog.Logger
}

func (s *storage) Save(ctx context.Context, links []model.Link) (uint64, error) {
	if s.saver != nil {
		return s.saver.Save(ctx, links)
	}

	resp, err := s.SaveBatch(ctx, [][]model.Link{links})
	if err != nil {
		return 0, err
	}
	return resp[0], nil
}

func (s *storage) SaveBatch(ctx context.Context, links [][]model.Link) ([]uint64, error) {
	const op = "Storage.SaveBatch"

	if len(links) == 0 {
		return nil, nil
	}

	// Сюда соберем сгенерированные IDs для новых записей
	ids := make([]uint64, len(links))

	// Сохраняем пачку запрос в базу одной транзакцией
	err := s.db.Update(func(tx Tx) error {
		b := tx.Bucket(LinkSetBucket)

		for i := range links {
			id, key, err := generateID(b)
			if err != nil {
				s.logger.Error("generate id failed", "op", op, "error", err)
				return model.ErrInternalError
			}

			ids[i] = id

			linkSet := model.LinkSet{
				ID:    id,
				Links: links[i],
			}

			data, err := json.Marshal(linkSet)
			if err != nil {
				s.logger.Error("marshal linkset failed", "op", op, "error", err)
				return model.ErrInternalError
			}

			if err := b.Put(key, data); err != nil {
				s.logger.Error("database write failed", "op", op, "error", err)
				return model.ErrInternalError
			}
		}

		return nil
	})

	if err != nil {
		return nil, err
	}

	if s.syncer != nil {
		s.syncer.Update()
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

func (s *storage) Load(ctx context.Context, id uint64) (model.LinkSet, error) {
	resp, err := s.LoadBatch(ctx, []uint64{id})
	if err != nil {
		return model.LinkSet{}, err
	}
	return resp.LinkSets[0], nil
}

func (s *storage) LoadBatch(ctx context.Context, ids []uint64) (LinkSetBatch, error) {
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

	err := s.db.View(func(tx Tx) error {
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

			if s.cache != nil {
				cloned := linkSets[i].Clone()
				s.cache.Put(&cloned)
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

var _ service.LinkStorage = &Storage{}
