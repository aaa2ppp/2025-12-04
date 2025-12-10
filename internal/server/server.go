package server

import (
	"cmp"
	"context"
	"log/slog"
	"net"
	"net/http"
	"strconv"
	"sync"
	"sync/atomic"
	"time"
)

const (
	DefaultReadTimeout        = 15 * time.Second
	DefaultWriteTimeout       = 15 * time.Second
	DefaultIdleTimeout        = 60 * time.Second
	DefaultShutdownTimeout    = 30 * time.Second
	DefaultControlShotTimeout = 100 * time.Millisecond
	DefaultRetryAfter         = 15
)

type Config struct {
	// Addr передается http.Server.
	Addr string

	// ReadTimeout передается http.Server. Если 0, будет установлено значение по умолчанию.
	ReadTimeout time.Duration

	// WriteTimeout передается http.Server. Если 0, будет установлено значение по умолчанию.
	WriteTimeout time.Duration

	// IdleTimeout передается http.Server. Если 0, будет установлено значение по умолчанию.
	IdleTimeout time.Duration

	// ShutdownTimeout время, в течение которого сервер ожидает завершения текущих задач при остановке.
	// Если 0, то используется значение по умолчанию. Если отрицательное, то не устанавливается.
	ShutdownTimeout time.Duration

	// ControlShotTimeout таймаут для "контрольного выстрела": время,
	// отводимое на принудительную остановку сервера после отмены базового контекста.
	// Если 0, то используется значение по умолчанию. Если отрицательное, то не устанавливается (не рекомендуется).
	ControlShotTimeout time.Duration

	// RetryAfter время в секундах, которое будет передано клиенту в заголовке 'Retry-After' при остановке сервера.
	// Если 0, то используется значение по умолчанию. Если отрицательное, то не передается.
	RetryAfter int
}

// Server обертка над [http.Server], обеспечивающая graceful shutdown, во время которого
// все новые запросы получают HTTP 503 + Retry-After.
type Server struct {
	server             *http.Server
	handler            http.Handler
	endImmediately     context.CancelFunc
	taskCount          atomic.Int64
	shuttingDown       atomic.Bool
	shutdownTimeout    time.Duration
	controlShotTimeout time.Duration
	retryAfter         int
	shutdownMu         sync.Mutex
}

// New создает новый Server с заданной конфигурацией и обработчиком.
//
// Паникует если handler == nil.
func New(cfg Config, handler http.Handler) *Server {
	if handler == nil {
		panic("server: handler cannot be nil")
	}

	ctx, cancel := context.WithCancel(context.Background())

	s := &Server{
		handler:            handler,
		endImmediately:     cancel,
		shutdownTimeout:    cmp.Or(cfg.ShutdownTimeout, DefaultShutdownTimeout),
		controlShotTimeout: cmp.Or(cfg.ControlShotTimeout, DefaultControlShotTimeout),
		retryAfter:         cmp.Or(cfg.RetryAfter, DefaultRetryAfter),
	}

	s.server = &http.Server{
		Addr:         cfg.Addr,
		ReadTimeout:  cmp.Or(cfg.ReadTimeout, DefaultReadTimeout),
		WriteTimeout: cmp.Or(cfg.WriteTimeout, DefaultWriteTimeout),
		IdleTimeout:  cmp.Or(cfg.IdleTimeout, DefaultIdleTimeout),
		Handler:      http.HandlerFunc(s.handle),
		BaseContext:  func(_ net.Listener) context.Context { return ctx },
	}

	return s
}

// Serve обертка над [http.Server.Serve] FOR TESTS ONLY.
func (s *Server) Serve(l net.Listener) error {
	return s.server.Serve(l)
}

// ListenAndServe обертка над [http.Server.ListenAndServe].
func (s *Server) ListenAndServe() error {
	slog.Info("server startup", "addr", s.server.Addr)
	return s.server.ListenAndServe()
}

func (s *Server) handle(w http.ResponseWriter, r *http.Request) {
	// NOTE: между проверкой shuttingDown и инкрементом taskCount
	// существует минимальное временное окно, в котором может произойти гонка.
	// Запрос, прошедший проверку, но зарегистрировавшийся после начала shutdown,
	// либо будет завершен штатно (если waitTasks еще ожидает),
	// либо получит немедленную отмену контекста (если waitTasks уже завершен).
	// Такие случаи крайне редки, а поведение согласовано с отказом в обработке
	// после истечения ShutdownTimeout, поэтому допустимы. В противном случае
	// необходимо делать повторную проверку shuttingDown после taskCount.Add или
	// использовать Mutex, что кажется избыточным.

	if s.shuttingDown.Load() {
		s.serverBusy(w)
		return
	}

	s.taskCount.Add(1)
	defer s.taskCount.Add(-1)

	s.handler.ServeHTTP(w, r)
}

func (s *Server) serverBusy(w http.ResponseWriter) {
	if s.retryAfter > 0 {
		w.Header().Set("Retry-After", strconv.Itoa(s.retryAfter))
	}
	http.Error(w, "server busy", http.StatusServiceUnavailable) // 503
}

// Shutdown обеспечивает graceful shutdown.
//
//   - Ждет завершения активных задач не более ShutdownTimeout.
//   - По таймауту отменяет базовый контекст и выполняет "контрольный выстрел" -
//     вызывает [http.Server.Shutdown] с таймаутом ControlShotTimeout.
//   - Все новые запросы получают HTTP 503 c заголовком 'Retry-After'.
//
// NOTE: При отмене контекста запроса, активная задача ОБЯЗАНА немедленно завершиться.
//
// Повторные вызовы возвращают [http.ErrServerClosed].
// Всегда, в том числе при повторных вызовах, возвращается ПОСЛЕ остановки сервера.
func (s *Server) Shutdown() error {
	slog.Info("server shutting down", "timeout", s.shutdownTimeout)

	s.shutdownMu.Lock()
	defer s.shutdownMu.Unlock()

	if !s.shuttingDown.CompareAndSwap(false, true) {
		return http.ErrServerClosed
	}

	ctx := context.Background()
	if s.shutdownTimeout > 0 {
		shutdownCtx, cancel := context.WithTimeout(ctx, s.shutdownTimeout)
		defer cancel()
		ctx = shutdownCtx
	}

	s.waitTasks(ctx)

	return s.close()
}

func (s *Server) waitTasks(ctx context.Context) {
	if s.taskCount.Load() == 0 {
		return
	}

	const tick = 50 * time.Millisecond

	tk := time.NewTicker(tick)
	defer tk.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-tk.C:
			if s.taskCount.Load() == 0 {
				return
			}
		}
	}
}

func (s *Server) close() error {
	slog.Debug("server send 'end immediately' to all task")
	s.endImmediately()

	ctx := context.Background()
	if s.controlShotTimeout > 0 {
		controlShotCtx, cancel := context.WithTimeout(ctx, s.controlShotTimeout)
		defer cancel()
		ctx = controlShotCtx
	}

	slog.Debug("server close")
	return s.server.Shutdown(ctx)
}
