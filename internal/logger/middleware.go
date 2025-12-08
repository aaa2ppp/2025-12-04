package logger

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"net/http"
	"runtime/debug"
	"time"
)

const ReqResultLevel = slog.LevelDebug

type requestInfo struct {
	ID       uint64
	From     string
	Method   string
	Path     string
	Took     time.Duration
	Status   int
	IOErr    error
	PanicErr error
}

// HTTPLogging создает middleware для логирования HTTP-запросов. Принимает логгер
// и следующий обработчик в цепочке, возвращает новый обработчик с логированием.
func HTTPLogging(log *slog.Logger, h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reqInfo := requestInfo{
			ID:     rand.Uint64(),
			From:   getClientIP(r),
			Method: r.Method,
			Path:   r.URL.Path,
		}

		// Генерируем уникальный ID для запроса и добавляем в логгер
		log := log.With("rid", reqInfo.ID)

		// Заменяем ResponseWriter на наш с хуком для логирования
		si := &statusInterceptor{
			ResponseWriter: w,
			log:            log,
		}

		// Добавляем логгер в контекст запроса
		ctx := Context(r.Context(), log)
		r = r.WithContext(ctx)

		// Засекааем время
		start := time.Now()

		defer func() {
			reqInfo.Took = time.Since(start)

			// Копируем статус и ошибку записи ДО recover
			reqInfo.Status = si.Status
			reqInfo.IOErr = si.StickyErr

			// Обрабатываем панику (если есть)
			if p := recover(); p != nil {
				log.Error("*** panic recovered ***", "panic", p, "stack", debug.Stack())

				// Если статус ещё не финальный — отправляем 500 клиенту
				if reqInfo.Status < 200 {
					http.Error(w, "internal error", http.StatusInternalServerError)
					reqInfo.Status = http.StatusInternalServerError
				}

				reqInfo.PanicErr = fmt.Errorf("panic: %v", p)
			}

			// Логируем результат — всегда, с полным контекстом
			logRequestResult(log, "request", reqInfo)
		}()

		// Передаем управление следующему обработчику
		h.ServeHTTP(si, r)
	})
}

// statusInterceptor логирует HTTP статусы и перехватывает ошибки
type statusInterceptor struct {
	http.ResponseWriter
	log *slog.Logger

	// Status — текущий HTTP-статус ответа.
	// 0 = не установлен (WriteHeader не вызывался),
	// 1xx = промежуточный статус (может быть перезаписан),
	// 2xx-5xx = финальный статус (последующие WriteHeader игнорируются).
	Status int

	StickyErr error
}

func (si *statusInterceptor) WriteHeader(status int) {
	switch {
	case status < 100:
		si.log.Debug("invalid status", "status", status)

	case si.Status < 200:
		si.Status = status
		si.ResponseWriter.WriteHeader(status)

	case si.Status != status:
		si.log.Warn("attempt to override final status code",
			"prevStatus", si.Status, "ignoredStatus", status)

	default:
		si.log.Debug("redundant WriteHeader call", "status", status)
	}
}

func (si *statusInterceptor) Write(b []byte) (int, error) {
	if si.StickyErr != nil {
		return 0, si.StickyErr
	}

	// Если статус ещё не установлен или промежуточный — Go (net/http) гарантирует,
	// что клиенту будет отправлен 200 при первом вызове Write.
	if si.Status < 200 {
		si.Status = 200
	}

	n, err := si.ResponseWriter.Write(b)
	if err != nil {
		si.StickyErr = err
	}

	return n, err
}

func logRequestResult(log *slog.Logger, tag string, info requestInfo) {
	if info.IOErr != nil || info.PanicErr != nil {
		log.Error(tag,
			"from", info.From,
			"method", info.Method,
			"path", info.Path,
			"took_us", info.Took.Microseconds(),
			"status", info.Status,
			"error", errors.Join(info.IOErr, info.PanicErr),
		)
	} else {
		log.Log(context.Background(), ReqResultLevel, tag,
			"from", info.From,
			"method", info.Method,
			"path", info.Path,
			"took_us", info.Took.Microseconds(),
			"status", info.Status,
		)
	}
}
