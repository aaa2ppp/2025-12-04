package checker

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"link-checker/internal/model"
	"link-checker/internal/service"
)

const (
	DefaultTimeout = 10 * time.Second
)

type Config struct {
	Timeout       time.Duration
	UserAgent     string
	TryHTTPSFirst bool
	// TryGETFallback bool // TODO
}

type Checker struct {
	client *http.Client
	config Config
}

func New(cfg Config) *Checker {
	if cfg.Timeout == 0 {
		cfg.Timeout = DefaultTimeout
	}
	if cfg.UserAgent == "" {
		cfg.UserAgent = "LinkChecker/1.0"
	}

	client := &http.Client{
		Timeout: cfg.Timeout,
		Transport: &http.Transport{
			MaxIdleConns:        100,
			MaxIdleConnsPerHost: 10,
			IdleConnTimeout:     90 * time.Second,
		},
	}

	return &Checker{
		client: client,
		config: cfg,
	}
}

func (c *Checker) Check(ctx context.Context, rawURLs []string) ([]model.Link, error) {
	links := make([]model.Link, len(rawURLs))
	var wg sync.WaitGroup

	for i, rawURL := range rawURLs {
		wg.Add(1)
		go func(i int, rawURL string) {
			defer wg.Done()
			links[i] = c.checkOne(ctx, rawURL)
		}(i, rawURL)
	}

	wg.Wait()
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
		// XXX: если не добавит, то "google.com" -> {schema:"", host:"", path:"google.com"}
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

	if c.config.TryHTTPSFirst {
		return []string{httpsURL, httpURL}, nil
	}
	return []string{httpURL, httpsURL}, nil
}

func (c *Checker) tryHEAD(ctx context.Context, url string) (statusCode int, _ error) {
	req, err := http.NewRequestWithContext(ctx, "HEAD", url, nil)
	if err != nil {
		return 0, fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("User-Agent", c.config.UserAgent)

	resp, err := c.client.Do(req)
	if err != nil {
		return 0, fmt.Errorf("network error: %w", err)
	}
	defer resp.Body.Close()

	statusCode = resp.StatusCode

	// TODO: Handle 405 Method Not Allowed
	// if statusCode == http.StatusMethodNotAllowed && c.config.TryGETFallback {
	// 	return c.tryGET(ctx, url)
	// }

	return statusCode, nil
}

func (c *Checker) isAvailable(statusCode int) bool {
	// TODO: уточнить условия доступности
	return statusCode >= 200 && statusCode < 300
}

func (c *Checker) tryGET(ctx context.Context, url string) (int, error) {
	// TODO
	return 0, errors.New("tryGET: unimplimented")
}

var _ service.URLChecker = &Checker{}
