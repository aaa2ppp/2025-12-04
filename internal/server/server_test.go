package server_test

import (
	"bytes"
	"errors"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/aaa2ppp/be"

	"link-checker/internal/server"
)

func noopHandler(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("ok"))
}

func newSlowHandler(timeout time.Duration) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-time.After(timeout):
			w.WriteHeader(http.StatusOK)
			w.Write([]byte("slow ok"))
		case <-r.Context().Done():
			w.WriteHeader(http.StatusServiceUnavailable)
			w.Write([]byte("cancelled"))
		}
	}
}

func startServer(cfg server.Config, h http.Handler) (_ *server.Server, url string, _ error) {
	s := server.New(cfg, h)

	l, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		return nil, "", err
	}

	go func() {
		s.Serve(l)
	}()

	time.Sleep(10 * time.Millisecond)
	url = "http://" + l.Addr().String() + "/"

	return s, url, nil
}

func shutdownServer(s *server.Server, timeout time.Duration) error {
	done := make(chan error)
	go func() {
		defer close(done)
		done <- s.Shutdown()
	}()

	select {
	case <-time.After(timeout):
		return errors.New("Shutdown did not complete in time: possible deadlock or leaked task")
	case err := <-done:
		return err
	}
}

func Test_NormalRequest(t *testing.T) {
	tests := []struct {
		name     string
		handler  http.Handler
		wantCode int
		wantBody string
	}{
		{
			"handler end before shutdown",
			http.HandlerFunc(noopHandler),
			200,
			"ok",
		},
		{
			"handler end after shutdown",
			newSlowHandler(100 * time.Millisecond),
			200,
			"slow ok",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s, url, err := startServer(server.Config{}, tt.handler)
			be.Err(t, err, nil)

			done := make(chan error)
			go func() {
				time.Sleep(10 * time.Millisecond)
				done <- shutdownServer(s, 200*time.Millisecond)
			}()

			resp, err := http.Get(url)
			be.Err(t, err, nil)

			body := bytes.Buffer{}
			_, err = body.ReadFrom(resp.Body)
			resp.Body.Close()
			be.Err(t, err, nil)

			be.Equal(t, resp.StatusCode, tt.wantCode)
			be.Equal(t, body.String(), tt.wantBody)

			be.Err(t, <-done, nil)
		})
	}
}

func Test_RequestDuringShutdown(t *testing.T) {
	s, url, err := startServer(
		server.Config{
			RetryAfter:      42,
			ShutdownTimeout: 100 * time.Millisecond,
		},
		http.HandlerFunc(newSlowHandler(500*time.Millisecond)),
	)
	be.Err(t, err, nil)

	// Первый запрос - его будет ждать Shutdown
	go func() {
		resp, err := http.Get(url)
		if err == nil {
			resp.Body.Close()
		}
	}()
	time.Sleep(10 * time.Millisecond)

	done := make(chan error, 1)
	go func() {
		done <- shutdownServer(s, 200*time.Millisecond)
	}()
	time.Sleep(10 * time.Millisecond)

	// Второй запрос во время Shutdown должен вернуть 503
	resp, err := http.Get(url)
	be.Err(t, err, nil)
	resp.Body.Close()

	be.Equal(t, resp.StatusCode, 503)
	be.Equal(t, resp.Header.Get("Retry-After"), "42")

	be.Err(t, <-done, nil)
}

func Test_ImmediateFinish(t *testing.T) {
	s, url, err := startServer(server.Config{
		ShutdownTimeout: 1 * time.Millisecond, // короткий, чтобы запрос не успел завершиться
	}, http.HandlerFunc(newSlowHandler(500*time.Millisecond)))
	be.Err(t, err, nil)

	done := make(chan error)
	go func() {
		time.Sleep(50 * time.Millisecond)
		done <- shutdownServer(s, 200*time.Millisecond)
	}()

	// Запрос прерываемый Shutdown должен поймать отмену контекста
	resp, err := http.Get(url)
	be.Err(t, err, nil)
	defer resp.Body.Close()

	body := bytes.Buffer{}
	_, err = body.ReadFrom(resp.Body)
	be.Err(t, err, nil)

	be.Equal(t, resp.StatusCode, http.StatusServiceUnavailable)
	be.Equal(t, body.String(), "cancelled")

	be.Err(t, <-done, nil)
}

func Test_CallAfterShutdown(t *testing.T) {
	s := server.New(server.Config{Addr: "localhost:0"}, http.HandlerFunc(noopHandler))

	go func() {
		s.ListenAndServe()
	}()

	time.Sleep(10 * time.Millisecond)

	err := shutdownServer(s, 200*time.Millisecond)
	be.Err(t, err, nil)

	// Повторный вызов должен вернуть http.ErrServerClosed
	be.Err(t, s.Shutdown(), http.ErrServerClosed)
	be.Err(t, s.ListenAndServe(), http.ErrServerClosed)
}
