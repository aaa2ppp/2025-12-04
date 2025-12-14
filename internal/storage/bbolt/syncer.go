package bbolt

import (
	"cmp"
	"log/slog"
	"math"
	"sync"
	"time"
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

type Syncer interface {
	Sync() error
}

type periodicSyncer struct {
	db                  Syncer
	logger              *slog.Logger
	maxNotSyncedUpdates int
	maxSyncDelay        time.Duration
	syncErrorLogPeriod  time.Duration
	lastErrorLog        time.Time
	updateCh            chan struct{}
	closeMu             sync.Mutex
	closeCh             chan struct{}
}

func newSyncer(db Syncer, logger *slog.Logger, cfg syncerConfig) *periodicSyncer {
	if db == nil {
		panic("newSyncer: db cannot be nil")
	}
	s := &periodicSyncer{
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
func (s *periodicSyncer) Close() {
	if s == nil {
		return
	}

	s.closeMu.Lock()
	defer s.closeMu.Unlock()

	select {
	case <-s.closeCh:
	default:
		close(s.closeCh)
	}
}

// Update сообщает синхронизатору, что было обновление. Ничего не делает, если ресивер nil или синхронизатор уже закрыт.
func (s *periodicSyncer) Update() {
	if s == nil {
		return
	}

	select {
	case <-s.closeCh:
	case s.updateCh <- struct{}{}:
	}
}

func (s *periodicSyncer) serve() {
	tm := time.NewTimer(math.MaxInt64)
	tm.Stop()
	defer tm.Stop()

	var (
		count int
	)

	for {
		select {
		case <-s.closeCh:
			return

		case <-tm.C:
			s.sync()
			count = 0

		case <-s.updateCh:
			count = (count + 1) % s.maxNotSyncedUpdates
			switch count {
			case 0:
				tm.Stop()
				s.sync()

			case 1:
				tm.Reset(s.maxSyncDelay)
			}
		}
	}
}

func (s *periodicSyncer) sync() {
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
