package bbolt

import (
	"context"
	"math"
	"sync"
	"time"
)

type syncFunc func() error

type lazySyncer struct {
	sync     syncFunc
	pending  int
	delay    time.Duration
	updateCh chan int
	syncCh   chan chan error
	closeMu  sync.Mutex
	closeCh  chan struct{}
}

func newLazySyncer(sync syncFunc, pending int, delay time.Duration) *lazySyncer {
	if sync == nil {
		panic("newLazySyncer: sync cannot be nil")
	}
	if delay <= 0 && pending <= 0 {
		panic("newLazySyncer: pending OR delay, at least one of them must be >0")
	}
	s := &lazySyncer{
		sync:    sync,
		pending: pending,
		delay:   delay,

		closeCh:  make(chan struct{}),
		syncCh:   make(chan chan error),
		updateCh: make(chan int),
	}
	go s.serve()
	return s
}

// Close прекращает работу синхронизатора. Ничего не делает, если или синхронизатор уже закрыт.
func (s *lazySyncer) Close() {
	s.closeMu.Lock()
	defer s.closeMu.Unlock()

	select {
	case <-s.closeCh:
	default:
		close(s.closeCh)
	}
}

// Sync выполняет принудительную синхронизацию. Ничего не делает, если синхронизатор уже закрыт.
func (s *lazySyncer) Sync(ctx context.Context) error {
	respCh := make(chan error, 1)
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-s.closeCh:
		return nil
	case s.syncCh <- respCh:
	}

	select {
	case <-ctx.Done():
		return ctx.Err()
	case err := <-respCh:
		return err
	}
}

// Update сообщает синхронизатору, что было обновление. Ничего не делает, если синхронизатор уже закрыт.
func (s *lazySyncer) Update(n int) {
	if n < 0 {
		panic("lazySyncer.Update: n must be >= 0")
	}
	if n == 0 {
		return
	}

	select {
	case <-s.closeCh:
	case s.updateCh <- n:
	}
}

func (s *lazySyncer) serve() {
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

		case respCh := <-s.syncCh:
			tm.Stop()
			respCh <- s.sync() // возвращаем ошибку инициатору
			count = 0

		case <-tm.C:
			s.sync()
			count = 0

		case n := <-s.updateCh:
			if s.pending > 0 && s.pending-n <= count {
				tm.Stop()
				s.sync()
				count = 0
				break
			}
			if s.delay > 0 && count == 0 {
				tm.Reset(s.delay)
			}
			count += n
		}
	}
}
