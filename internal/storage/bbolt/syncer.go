package bbolt

import (
	"cmp"
	"log/slog"
	"math"
	"time"

	"go.etcd.io/bbolt"
)

const (
	DefaultMaxNotSyncedUpdates = 10000
	DefaultMaxSyncDelay        = 5 * time.Second
	DefaultSyncErrorLogPeriod  = 1 * time.Minute
)

type syncerConfig struct {
	maxNotSyncedUpdates int
	maxSyncDelay        time.Duration
	syncErrorLogPeriod  time.Duration
}

type syncer struct {
	db                  *bbolt.DB
	logger              *slog.Logger
	maxNotSyncedUpdates int
	maxSyncDelay        time.Duration
	syncErrorLogPeriod  time.Duration
	lastErrorLog        time.Time
	closeCh             chan struct{}
	updateCh            chan struct{}
}

func newSyncer(db *bbolt.DB, logger *slog.Logger, cfg syncerConfig) *syncer {
	if db == nil {
		panic("newSyncer: db cannot be nil")
	}
	s := &syncer{
		db:     db,
		logger: logger,

		maxNotSyncedUpdates: cmp.Or(max(cfg.maxNotSyncedUpdates, 0), DefaultMaxNotSyncedUpdates),
		maxSyncDelay:        cmp.Or(max(cfg.maxSyncDelay, 0), DefaultMaxSyncDelay),
		syncErrorLogPeriod:  cmp.Or(max(cfg.syncErrorLogPeriod, 0), DefaultSyncErrorLogPeriod),

		closeCh:  make(chan struct{}),
		updateCh: make(chan struct{}),
	}
	go s.serve()
	return s
}

// Close прекращает работу синхронизатора. Ничего не делает, если рессивер nil или синхронизатор уже закрыт.
func (s *syncer) Close() {
	if s != nil {
		close(s.closeCh)
	}
}

// Update сообщает синхронизатору, что было обновление. Ничего не делает, если ресивер nil или синхронизатор уже закрыт.
func (s *syncer) Update() {
	if s != nil {
		select {
		case s.updateCh <- struct{}{}:
		case <-s.closeCh:
			// syncer закрыт, игнорируем
		}
	}
}

func (s *syncer) serve() {
	tm := time.NewTimer(time.Duration(math.MaxInt))
	tm.Stop()
	var c <-chan time.Time
	var count int

	defer tm.Stop()

	for {
		select {
		case <-s.closeCh:
			return

		case <-c:
			// Таймер сработал
			c = nil   // инвариант: остановка -> c = nil
			count = 0 // инвариант: синхронизация -> count = 0
			s.sync()

		case <-s.updateCh:
			count = (count + 1) % s.maxNotSyncedUpdates
			switch count {
			case 0:
				// Ручная остановка (достигли лимита обновлений)
				tm.Stop()
				c = nil   // инвариант: остановка -> c = nil
				count = 0 // уже 0, но для ясности
				s.sync()

			case 1:
				// Запускаем таймер отложенной синхронизации
				tm.Reset(s.maxSyncDelay)
				c = tm.C // инвариант: запуск -> c = tm.C
			}
		}
	}
}

func (s *syncer) sync() {
	if err := s.db.Sync(); err != nil {
		if s.logger == nil {
			return
		}
		if now := time.Now(); now.Sub(s.lastErrorLog) >= s.syncErrorLogPeriod {
			s.logger.Error("db sync failed", "error", err)
			s.lastErrorLog = now
		}
	}
}
