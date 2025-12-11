package bbolt

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"link-checker/internal/model"
	"log/slog"
	"math"
	"sync"
	"time"

	"go.etcd.io/bbolt"
)

const (
	DefaultMaxSaveQueueSize = 1000
	DefaultMaxSaveDelay     = 100 * time.Millisecond
	GenerateIDAttempts      = 10
)

var (
	ErrSaverClosed = errors.New("saver closed")
)

type saverConfig struct {
	maxQueueSize int
	maxDelay     time.Duration
	syncer       *syncer
	cache        *cache
}

type saveReq struct {
	links  []model.Link
	respCh chan<- saveResp
}

type saveResp struct {
	id  uint64
	err error
}

type saver struct {
	db           *bbolt.DB
	logger       *slog.Logger
	syncer       *syncer
	cache        *cache
	maxQueueSize int
	maxDelay     time.Duration
	saveCh       chan saveReq
	closeCh      chan struct{}
	done         chan struct{}
	closeMu      sync.Mutex
}

func newSaver(db *bbolt.DB, logger *slog.Logger, cfg saverConfig) *saver {
	if db == nil {
		panic("newSaver: db cannot be nil")
	}
	if logger == nil {
		logger = slog.New(slog.DiscardHandler)
	}

	s := &saver{
		db:     db,
		logger: logger,

		syncer:       cfg.syncer,
		cache:        cfg.cache,
		maxQueueSize: cmp.Or(max(cfg.maxQueueSize, 0), DefaultMaxSaveQueueSize),
		maxDelay:     cmp.Or(max(cfg.maxDelay, 0), DefaultMaxSaveDelay),

		saveCh:  make(chan saveReq),
		closeCh: make(chan struct{}),
		done:    make(chan struct{}),
	}
	go s.serve()
	return s
}

func (s *saver) Close() error {
	s.closeMu.Lock()
	defer s.closeMu.Unlock()

	select {
	case <-s.closeCh:
		return ErrSaverClosed
	default:
	}

	close(s.closeCh)
	<-s.done

	return nil
}

func (s *saver) Save(ctx context.Context, links []model.Link) (uint64, error) {
	// ждем пока прочтут запрос или отмены контекста
	respCh := make(chan saveResp, 1)
	select {
	case <-s.closeCh:
		return 0, ErrSaverClosed
	case <-ctx.Done():
		return 0, ctx.Err()
	case s.saveCh <- saveReq{links: links, respCh: respCh}:
	}

	// ждем пока вернут ответ или отмены контекста
	var resp saveResp
	select {
	case <-ctx.Done():
		return 0, ctx.Err()
	case resp = <-respCh:
	}

	return resp.id, resp.err
}

func (s *saver) serve() {
	defer close(s.done)

	// создаем остановленный таймар
	tm := time.NewTimer(math.MaxInt64)
	if !tm.Stop() {
		<-tm.C // избыточно для v1.23+
	}
	defer tm.Stop()

	queue := make([]saveReq, 0, s.maxQueueSize)

	for {
		select {
		case <-s.closeCh:
			s.saveQueue(queue)
			return

		case <-tm.C:
			s.saveQueue(queue)
			queue = queue[:0]

		case req := <-s.saveCh:
			queue = append(queue, req)
			switch {
			case len(queue) >= s.maxQueueSize:
				if !tm.Stop() {
					<-tm.C // избыточно для v1.23+
				}
				s.saveQueue(queue)
				queue = queue[:0]
			case len(queue) == 1:
				tm.Reset(s.maxDelay)
			}
		}
	}
}

func (s *saver) saveQueue(queue []saveReq) {
	const op = "saver.saveQueue"

	if len(queue) == 0 {
		return
	}

	ids := make([]uint64, 0, len(queue))

	// Сохраняем пачку в базу в одной транзакции
	err := s.db.Update(func(tx *bbolt.Tx) error {
		b := tx.Bucket(LinkSetBucket)

		for i := range queue {
			id, key, err := generateID(b)
			if err != nil {
				s.logger.Error("generate id failed", "op", op, "error", err)
				return model.ErrInternalError
			}

			ids = append(ids, id)

			linkSet := model.LinkSet{
				ID:    id,
				Links: model.CloneLinks(queue[i].links),
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
		for i := range queue {
			queue[i].respCh <- saveResp{id: 0, err: err}
		}
		return
	}

	// Сообщаем синхронизатору об обновлении
	if s.syncer != nil {
		s.syncer.Update()
	}

	// Обновляем кеш
	if s.cache != nil {
		s.cache.Do(func(c lockedCache) {
			for i := range queue {
				linkSet := &model.LinkSet{
					ID:    ids[i],
					Links: model.CloneLinks(queue[i].links),
				}
				c.Set(linkSet)
			}
		})
	}

	// Возвращаем результат инициаторам
	for i := range queue {
		queue[i].respCh <- saveResp{id: ids[i], err: nil}
	}
}
