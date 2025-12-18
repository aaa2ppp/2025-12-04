package app_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/aaa2ppp/be"

	"link-checker/internal/app"
	"link-checker/internal/config"
)

func startApp(ctx context.Context, cfg config.Config) func() error {
	ctx, stop := context.WithCancel(ctx)
	done := make(chan error, 1)
	go func() {
		defer close(done)
		done <- new(app.App).Run(ctx, cfg)
	}()
	return func() error {
		stop()
		return <-done
	}
}

type client struct {
	client  *http.Client
	baseURL string
}

func (c *client) waitPing(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, "GET", c.baseURL+"/ping", nil)
	if err != nil {
		return err
	}

	for ctx.Err() == nil {
		resp, err := c.client.Do(req)
		if err == nil {
			io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				return fmt.Errorf("unxpected status code %d", resp.StatusCode)
			}
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}

	return ctx.Err()
}

func (c *client) postCheckLinks(ctx context.Context, reqBody string) (uint64, error) {
	req, err := http.NewRequestWithContext(ctx, "POST", c.baseURL+"/api/check-links", strings.NewReader(reqBody))
	if err != nil {
		return 0, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("unxpected status code %d", resp.StatusCode)
	}

	var checkResp struct {
		LinksNum uint64 `json:"links_num"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&checkResp); err != nil {
		return 0, err
	}

	return checkResp.LinksNum, nil
}

func (c *client) postGetReport(ctx context.Context, reqBody string) error {
	req, err := http.NewRequestWithContext(ctx, "POST", c.baseURL+"/api/get-report", strings.NewReader(reqBody))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unxpected status code %d", resp.StatusCode)
	}

	if ct := resp.Header.Get("Content-Type"); ct != "application/pdf" {
		return fmt.Errorf("unxpected content type %s", ct)
	}

	pdfData, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if len(pdfData) < 4 && string(pdfData[:4]) != "%PDF" {
		return fmt.Errorf("response body is not PDF")
	}

	return nil
}

func TestIntegration_FullFlow(t *testing.T) {
	tmpDir := t.TempDir()

	var ge config.Getenv
	port := ge.String("PORT", false, "63777")
	logLevel := ge.LogLevel("LOG_LEVEL", false, slog.LevelDebug)

	addr := "localhost:" + port
	baseURL := "http://localhost:" + port

	dataFile := filepath.Join(tmpDir, "test.db")

	cfg := config.Config{
		Logger:    config.Logger{Level: logLevel},
		Server:    config.Server{Addr: addr, ShutdownTimeout: 5 * time.Second},
		BBoltStor: config.BBoltStor{DataFile: dataFile, CacheSize: 100},
		Checker:   config.Checker{Timeout: 2 * time.Second},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	stopApp := startApp(ctx, cfg)

	c := client{
		client:  &http.Client{Timeout: 5 * time.Second},
		baseURL: baseURL,
	}

	be.Err(t, c.waitPing(ctx), nil)

	linksNum, err := c.postCheckLinks(ctx, `{"links":["https://httpbin.org/status/200","https://httpbin.org/status/404"]}`)
	be.Err(t, err, nil)

	be.Err(t, c.postGetReport(ctx, fmt.Sprintf(`{"links_list":[%d]}`, linksNum)), nil)

	be.Err(t, stopApp(), nil)

	_, err = os.Stat(dataFile)
	be.Err(t, err, nil)
}

func TestIntegration_Restart(t *testing.T) {
	tmpDir := t.TempDir()

	var ge config.Getenv
	port := ge.String("PORT", false, "63777")
	logLevel := ge.LogLevel("LOG_LEVEL", false, slog.LevelDebug)

	addr := "localhost:" + port
	baseURL := "http://localhost:" + port

	dataFile := filepath.Join(tmpDir, "test.db")

	cfg := config.Config{
		Logger:    config.Logger{Level: logLevel},
		Server:    config.Server{Addr: addr, ShutdownTimeout: 5 * time.Second},
		BBoltStor: config.BBoltStor{DataFile: dataFile, CacheSize: 100},
		Checker:   config.Checker{Timeout: 2 * time.Second},
	}

	c := client{
		client:  &http.Client{Timeout: 5 * time.Second},
		baseURL: baseURL,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	// --- 1. Первый запуск: создаём запись ---
	linksNum := func() uint64 {
		stopApp := startApp(ctx, cfg)
		defer stopApp()

		be.Err(t, c.waitPing(ctx), nil)
		linksNum, err := c.postCheckLinks(ctx, `{"links":["https://httpbin.org/status/200","https://httpbin.org/status/404"]}`)
		be.Err(t, err, nil)

		be.Err(t, stopApp(), nil)
		return linksNum
	}()

	// --- 2. Второй запуск: читаем ту же запись ---
	func() {
		stopApp := startApp(ctx, cfg)
		defer stopApp()

		be.Err(t, c.waitPing(ctx), nil)
		be.Err(t, c.postGetReport(ctx, fmt.Sprintf(`{"links_list":[%d]}`, linksNum)), nil)

		be.Err(t, stopApp(), nil)
	}()
}
