package logger

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"

	"github.com/aaa2ppp/be"
)

// logHandlerMock — накапливает записи лога
type logHandlerMock struct {
	attrs    []slog.Attr
	records  []testRecord
	disabled bool
}

type testRecord struct {
	Level   slog.Level
	Message string
	Attrs   []slog.Attr
}

func (h *logHandlerMock) Enabled(context.Context, slog.Level) bool {
	return !h.disabled
}

func (h *logHandlerMock) Handle(_ context.Context, r slog.Record) error {
	attrs := slices.Clone(h.attrs)
	r.Attrs(func(a slog.Attr) bool {
		attrs = append(attrs, a)
		return true
	})
	h.records = append(h.records, testRecord{
		Level:   r.Level,
		Message: r.Message,
		Attrs:   attrs,
	})
	return nil
}

func (h *logHandlerMock) WithAttrs(attrs []slog.Attr) slog.Handler {
	// TEST-ONLY: modifies shared state for simplicity.
	// This is safe ONLY because:
	//   - Each test creates its own logHandlerMock
	//   - No parallel execution within test
	//   - Not used to test WithAttrs behavior
	//
	// NEVER do this in production — violates slog.Handler contract.
	// In prod, WithAttrs must return a NEW handler with copied attrs.
	h.attrs = append(h.attrs, attrs...)
	return h
}

func (h *logHandlerMock) WithGroup(name string) slog.Handler { return h }

func TestHTTPLogging(t *testing.T) {
	t.Run("successful request", func(t *testing.T) {
		h := &logHandlerMock{}
		log := slog.New(h)

		handler := HTTPLogging(log, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			w.Write([]byte("OK"))
		}))

		req := httptest.NewRequest("GET", "/api/data", nil)
		req.RemoteAddr = "192.168.1.1:12345"
		w := httptest.NewRecorder()

		handler.ServeHTTP(w, req)

		be.Equal(be.Require(t), len(h.records), 1)
		r := h.records[0]

		be.Equal(t, r.Message, "request")
		be.Equal(t, r.Level, ReqResultLevel)

		attrs := attrMap(r.Attrs)
		be.True(t, attrs["rid"] != nil) // rid should be present
		be.Equal(t, attrs["from"], "192.168.1.1")
		be.Equal(t, attrs["method"], "GET")
		be.Equal(t, attrs["path"], "/api/data")
		status, _ := attrs["status"].(int64) // NOTE: slog превращает int -> int64, uint -> uint64
		be.Equal(t, status, 200)
		took_us, _ := attrs["took_us"].(int64)
		be.True(t, took_us >= 0) // took_us should be >= 0
		be.Equal(t, keyOrMissing(attrs, "error"), "<missing>")
	})

	t.Run("request with panic", func(t *testing.T) {
		h := &logHandlerMock{}
		log := slog.New(h)

		handler := HTTPLogging(log, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			panic("something went wrong")
		}))

		req := httptest.NewRequest("GET", "/api/data", nil)
		w := httptest.NewRecorder()

		handler.ServeHTTP(w, req)

		// Первая запись — паника
		be.True(be.Require(t), len(h.records) >= 1)
		panicRecord := h.records[0]
		be.Equal(t, panicRecord.Message, "*** panic recovered ***")
		be.Equal(t, panicRecord.Level, slog.LevelError)

		// Вторая запись — результат запроса
		be.Equal(be.Require(t), len(h.records), 2)
		resultRecord := h.records[1]
		be.Equal(t, resultRecord.Message, "request")
		be.Equal(t, resultRecord.Level, slog.LevelError)

		attrs := attrMap(resultRecord.Attrs)
		status, _ := attrs["status"].(int64)
		be.Equal(t, status, 500)
		err, _ := attrs["error"].(error)
		be.Err(t, err, "panic: something went wrong")
	})

	t.Run("request with write error", func(t *testing.T) {
		h := &logHandlerMock{}
		log := slog.New(h)

		failingWriter := &failingResponseWriter{}

		handler := HTTPLogging(log, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			w.Write([]byte("OK")) // вернёт ошибку
		}))

		req := httptest.NewRequest("GET", "/api/data", nil)
		w := failingWriter

		handler.ServeHTTP(w, req)

		be.Equal(be.Require(t), len(h.records), 1)
		r := h.records[0]

		be.Equal(t, r.Level, slog.LevelError)
		attrs := attrMap(r.Attrs)
		err, _ := attrs["error"].(error)
		be.Err(t, err, "simulated write error")
	})

	t.Run("logger in context", func(t *testing.T) {
		h := &logHandlerMock{}
		log := slog.New(h)

		handler := HTTPLogging(log, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			l := FromContext(r.Context())
			l.Info("inside handler")
			w.WriteHeader(http.StatusOK)
		}))

		req := httptest.NewRequest("GET", "/api/data", nil)
		w := httptest.NewRecorder()

		handler.ServeHTTP(w, req)

		// Первая запись — внутри обработчика
		be.True(be.Require(t), len(h.records) >= 1)
		inner := h.records[0]
		be.Equal(t, inner.Message, "inside handler")
		be.Equal(t, inner.Level, slog.LevelInfo)

		// Вторая запись — результат запроса
		be.Equal(be.Require(t), len(h.records), 2)
		result := h.records[1]
		be.Equal(t, result.Message, "request")
	})

	t.Run("automatic 200 on Write", func(t *testing.T) {
		h := &logHandlerMock{}
		log := slog.New(h)

		handler := HTTPLogging(log, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Write([]byte("OK")) // WriteHeader не вызван — status должен быть 200
		}))

		req := httptest.NewRequest("GET", "/api/data", nil)
		w := httptest.NewRecorder()

		handler.ServeHTTP(w, req)

		resp := w.Result()
		be.Equal(t, resp.StatusCode, 200)

		be.Equal(be.Require(t), len(h.records), 1)
		attrs := attrMap(h.records[0].Attrs)

		status, _ := attrs["status"].(int64)
		be.Equal(t, status, 200)
	})
}

// attrMap converts []slog.Attr to map[string]any
func attrMap(attrs []slog.Attr) map[string]any {
	m := make(map[string]any)
	for _, a := range attrs {
		m[a.Key] = a.Value.Any()
	}
	return m
}

// keyOrMissing returns key if it exists in map, otherwise "<missing>"
func keyOrMissing(m map[string]any, key string) string {
	if _, ok := m[key]; ok {
		return key
	}
	return "<missing>"
}

// failingResponseWriter — мок, который возвращает ошибку при Write
type failingResponseWriter struct {
	http.ResponseWriter
}

func (f *failingResponseWriter) WriteHeader(status int) {
	// игнорируем
}

func (f *failingResponseWriter) Write(b []byte) (int, error) {
	return 0, errors.New("simulated write error")
}
