package server

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/aaa2ppp/be"
)

func TestNew(t *testing.T) {
	tests := []struct {
		name                   string
		cfg                    Config
		wantReadTimeout        time.Duration
		wantWriteTimeout       time.Duration
		wantIdleTimeout        time.Duration
		wantShutdownTimeout    time.Duration
		wantControlShotTimeout time.Duration
		wantRetryAfter         int
		wantAddr               string
	}{
		{
			name:                   "defaults from zero config",
			cfg:                    Config{},
			wantReadTimeout:        DefaultReadTimeout,
			wantWriteTimeout:       DefaultWriteTimeout,
			wantIdleTimeout:        DefaultIdleTimeout,
			wantShutdownTimeout:    DefaultShutdownTimeout,
			wantControlShotTimeout: DefaultControlShotTimeout,
			wantRetryAfter:         int(DefaultRetryAfter),
			wantAddr:               "",
		},
		{
			name: "custom config",
			cfg: Config{
				Addr:               ":12345",
				ReadTimeout:        5 * time.Second,
				WriteTimeout:       6 * time.Second,
				IdleTimeout:        7 * time.Second,
				ShutdownTimeout:    8 * time.Second,
				ControlShotTimeout: 9 * time.Millisecond,
				RetryAfter:         42,
			},
			wantReadTimeout:        5 * time.Second,
			wantWriteTimeout:       6 * time.Second,
			wantIdleTimeout:        7 * time.Second,
			wantShutdownTimeout:    8 * time.Second,
			wantControlShotTimeout: 9 * time.Millisecond,
			wantRetryAfter:         42,
			wantAddr:               ":12345",
		},
		{
			name: "negative values preserved",
			cfg: Config{
				ShutdownTimeout:    -5 * time.Second,
				ControlShotTimeout: -100 * time.Millisecond,
				RetryAfter:         -42,
			},
			wantReadTimeout:        DefaultReadTimeout,
			wantWriteTimeout:       DefaultWriteTimeout,
			wantIdleTimeout:        DefaultIdleTimeout,
			wantShutdownTimeout:    -5 * time.Second,
			wantControlShotTimeout: -100 * time.Millisecond,
			wantRetryAfter:         -42,
			wantAddr:               "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := New(tt.cfg, http.DefaultServeMux)

			be.Equal(t, s.server.Addr, tt.wantAddr)
			be.Equal(t, s.server.ReadTimeout, tt.wantReadTimeout)
			be.Equal(t, s.server.WriteTimeout, tt.wantWriteTimeout)
			be.Equal(t, s.server.IdleTimeout, tt.wantIdleTimeout)
			be.Equal(t, s.shutdownTimeout, tt.wantShutdownTimeout)
			be.Equal(t, s.controlShotTimeout, tt.wantControlShotTimeout)
			be.Equal(t, s.retryAfter, tt.wantRetryAfter)
		})
	}
}

func TestServer_handle_BlocksWhenShuttingDown(t *testing.T) {
	tests := []struct {
		retryAfter int
		wantHeader string
	}{
		{42, "42"},
		{0, strconv.Itoa(int(DefaultRetryAfter))},
		{-1, ""},
	}

	for _, tt := range tests {
		t.Run("RetryAfter="+strconv.Itoa(tt.retryAfter), func(t *testing.T) {
			var called bool
			handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				called = true
			})

			s := New(Config{RetryAfter: tt.retryAfter}, handler)

			// Прямая модификация внутреннего состояния — допустимо только здесь
			s.shuttingDown.Store(true)

			rec := httptest.NewRecorder()
			req := httptest.NewRequest("GET", "/", nil)
			s.handle(rec, req)

			be.Equal(t, rec.Code, http.StatusServiceUnavailable)
			be.Equal(t, rec.Header().Get("Retry-After"), tt.wantHeader)
			be.True(t, !called)
		})
	}
}
