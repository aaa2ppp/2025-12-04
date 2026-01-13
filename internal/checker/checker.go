package checker

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync/atomic"
	"time"

	"link-checker/internal/model"
	"link-checker/internal/service"
)

const (
	DefaultUserAgent   = "LinkChecker/1.0"
	DefaultMaxRequests = 20

	DefaultTimeout             = 10 * time.Second
	DefaultMaxIdleConns        = 100
	DefaultMaxIdleConnsPerHost = 10
	DefaultIdleConnTimeout     = 90 * time.Second

	maxWorkerIdle = 10 * time.Second
)

var (
	ErrClosed = errors.New("checker closed")
)

type Config struct {
	UserAgent   string
	MaxRequests int

	// http client config
	Timeout             time.Duration
	TryHTTPSFirst       bool
	TryGETFallback      bool // сейчас игнорируется, всегда false
	MaxIdleConns        int
	MaxIdleConnsPerHost int
	IdleConnTimeout     time.Duration
}

type Checker struct {
	client         *http.Client
	userAgent      string
	tryHTTPSFirst  bool
	tryGETFallback bool
	workerSlots    chan struct{}
	workerCount    int
	maxWorkerIdle  time.Duration
	jobs           chan job
	closed         atomic.Bool
}

func New(cfg Config) *Checker {
	client := &http.Client{
		Timeout: cmp.Or(cfg.Timeout, DefaultTimeout),
		Transport: &http.Transport{
			MaxIdleConns:        cmp.Or(cfg.MaxIdleConns, DefaultMaxIdleConns),
			MaxIdleConnsPerHost: cmp.Or(cfg.MaxIdleConnsPerHost, DefaultMaxIdleConns),
			IdleConnTimeout:     cmp.Or(cfg.IdleConnTimeout, DefaultIdleConnTimeout),
		},
	}

	workerCount := cfg.MaxRequests
	if workerCount <= 0 {
		workerCount = DefaultMaxRequests
	}
	workerSlots := make(chan struct{}, workerCount)
	for i := 0; i < workerCount; i++ {
		workerSlots <- struct{}{}
	}

	return &Checker{
		client:         client,
		userAgent:      cmp.Or(cfg.UserAgent, DefaultUserAgent),
		tryHTTPSFirst:  cfg.TryHTTPSFirst,
		tryGETFallback: false, // TODO: должно быть cfg.TryGETFallback после реализации tryGET()
		workerSlots:    workerSlots,
		workerCount:    workerCount,
		jobs:           make(chan job),
		maxWorkerIdle:  maxWorkerIdle,
	}
}

func (c *Checker) Close() error {
	if c.closed.Swap(true) {
		return ErrClosed
	}
	c.closed.Store(true)
	close(c.jobs)
	for i := 0; i < c.workerCount; i++ {
		<-c.workerSlots
	}
	return nil
}

type job struct {
	ctx     context.Context
	checker *Checker
	rawURL  string
	result  *model.Link
	done    chan<- struct{}
}

func (j *job) run() {
	*j.result = j.checker.checkOne(j.ctx, j.rawURL)
	j.done <- struct{}{}
}

func (c *Checker) Check(ctx context.Context, rawURLs []string) ([]model.Link, error) {
	if c.closed.Load() {
		return nil, ErrClosed
	}

	links := make([]model.Link, len(rawURLs))
	done := make(chan struct{}, len(rawURLs))

	for i, rawURL := range rawURLs {
		job := job{
			ctx:     ctx,
			checker: c,
			rawURL:  rawURL,
			result:  &links[i],
			done:    done,
		}

		select {
		case c.jobs <- job:
		default:
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case c.jobs <- job:
			case <-c.workerSlots:
				go func() {
					defer func() {
						c.workerSlots <- struct{}{}
					}()
					runWorker(job, c.maxWorkerIdle, c.jobs)
				}()
			}
		}
	}

	for i := 0; i < len(links); i++ {
		<-done
	}

	return links, nil
}

func (c *Checker) checkOne(ctx context.Context, rawURL string) model.Link {
	urlsToTry, err := c.prepareURLsToTry(rawURL)
	if err != nil {
		return model.Link{
			Name:   rawURL,
			Reason: err.Error(),
		}
	}

	var (
		lastURL        string
		lastStatusCode int
	)

	// Пробуем схемы по порядку
	for _, url := range urlsToTry {
		statusCode, err := c.tryHEAD(ctx, url)

		if err == nil && statusCode == http.StatusMethodNotAllowed && c.tryGETFallback {
			statusCode, err = c.tryGET(ctx, url)
		}

		if err != nil {
			// Network error, context canceled, etc - смена схемы не поможет
			return model.Link{
				Name:   rawURL,
				URL:    url,
				Reason: err.Error(),
			}
		}

		if c.isAvailable(statusCode) {
			// bingo!
			return model.Link{
				Name:       rawURL,
				URL:        url,
				StatusCode: statusCode,
				Available:  true,
			}
		}

		lastStatusCode = statusCode
		lastURL = url
	}

	if lastStatusCode == 0 {
		return model.Link{
			Name:   rawURL,
			Reason: "no any HTTP response received",
		}
	}

	return model.Link{
		Name:       rawURL,
		URL:        lastURL,
		StatusCode: lastStatusCode,
		Reason:     fmt.Sprintf("HTTP %d %s", lastStatusCode, http.StatusText(lastStatusCode)),
	}
}

// prepareURLsToTry парсит URL и возвращает варианты для проверки или ошибку.
//
// Пример: "example.com" -> ["https://example.com", "http://example.com"] (если TryHTTPSFirst=true)
func (c *Checker) prepareURLsToTry(raw string) ([]string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, fmt.Errorf("empty URL")
	}

	u, err := url.Parse(raw)
	if err != nil {
		return nil, err
	}

	if u.Scheme == "" {
		// XXX: если не добавить, то "google.com" -> {schema:"", host:"", path:"google.com"}
		u, err = url.Parse("//" + raw)
		if err != nil {
			return nil, err
		}
	}

	if u.Host == "" {
		return nil, fmt.Errorf("missing host")
	}

	if u.Scheme != "" {
		if u.Scheme != "http" && u.Scheme != "https" {
			return nil, fmt.Errorf("unsupported scheme: %s", u.Scheme)
		}
		return []string{raw}, nil
	}

	u.Scheme = "http"
	httpURL := u.String()

	u.Scheme = "https"
	httpsURL := u.String()

	if c.tryHTTPSFirst {
		return []string{httpsURL, httpURL}, nil
	}
	return []string{httpURL, httpsURL}, nil
}

func (c *Checker) tryHEAD(ctx context.Context, url string) (statusCode int, _ error) {
	req, err := http.NewRequestWithContext(ctx, "HEAD", url, nil)
	if err != nil {
		return 0, fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("User-Agent", c.userAgent)

	resp, err := c.client.Do(req)
	if err != nil {
		return 0, fmt.Errorf("network error: %w", err)
	}
	defer resp.Body.Close()

	statusCode = resp.StatusCode

	return statusCode, nil
}

func (c *Checker) tryGET(ctx context.Context, url string) (int, error) {
	// TODO
	return 0, errors.New("tryGET: unimplimented")
}

func (c *Checker) isAvailable(statusCode int) bool {
	// TODO: уточнить условия доступности
	return statusCode >= 200 && statusCode < 300
}

var _ service.URLChecker = &Checker{}
