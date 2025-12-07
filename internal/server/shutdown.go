package server

import (
	"context"
	"net/http"
	"sync"
	"sync/atomic"
)

type shutdownMiddleware struct {
	handler      http.Handler
	wg           sync.WaitGroup
	shuttingDown atomic.Bool
}

func newShutdownMiddleware(h http.Handler) *shutdownMiddleware {
	return &shutdownMiddleware{handler: h}
}

func (s *shutdownMiddleware) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if s.shuttingDown.Load() {
		c := http.StatusServiceUnavailable // 503
		http.Error(w, http.StatusText(c), c)
		return
	}

	s.wg.Add(1)
	defer s.wg.Done()

	if s.shuttingDown.Load() {
		c := http.StatusServiceUnavailable
		http.Error(w, http.StatusText(c), c)
		return
	}

	s.handler.ServeHTTP(w, r)
}

func (s *shutdownMiddleware) Shutdown(ctx context.Context) error {
	if !s.shuttingDown.CompareAndSwap(false, true) {
		return http.ErrServerClosed
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		s.wg.Wait()
	}()

	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

var _ http.Handler = &shutdownMiddleware{}
