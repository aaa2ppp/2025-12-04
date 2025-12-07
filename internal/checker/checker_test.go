package checker

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/aaa2ppp/be"
)

func TestChecker_prepareURLsToTry(t *testing.T) {
	tests := []struct {
		name     string
		config   Config
		rawURL   string
		wantURLs []string
		wantErr  bool
	}{
		{
			name: "URL without scheme, HTTPS first",
			config: Config{
				TryHTTPSFirst: true,
			},
			rawURL:   "example.com",
			wantURLs: []string{"https://example.com", "http://example.com"},
		},
		{
			name: "URL without scheme, HTTP first",
			config: Config{
				TryHTTPSFirst: false,
			},
			rawURL:   "example.com",
			wantURLs: []string{"http://example.com", "https://example.com"},
		},
		{
			name: "URL with HTTP scheme, HTTPS first config",
			config: Config{
				TryHTTPSFirst: true,
			},
			rawURL:   "http://example.com",
			wantURLs: []string{"http://example.com"},
		},
		{
			name: "URL with HTTPS scheme, HTTP first config",
			config: Config{
				TryHTTPSFirst: false,
			},
			rawURL:   "https://example.com",
			wantURLs: []string{"https://example.com"},
		},
		{
			name: "URL with path and query",
			config: Config{
				TryHTTPSFirst: true,
			},
			rawURL:   "example.com/path?query=1#fragment",
			wantURLs: []string{"https://example.com/path?query=1#fragment", "http://example.com/path?query=1#fragment"},
		},
		{
			name: "empty URL",
			config: Config{
				TryHTTPSFirst: true,
			},
			rawURL:  "",
			wantErr: true,
		},
		{
			name: "URL with unsupported scheme",
			config: Config{
				TryHTTPSFirst: true,
			},
			rawURL:  "ftp://example.com",
			wantErr: true,
		},
		{
			name: "URL without host",
			config: Config{
				TryHTTPSFirst: true,
			},
			rawURL:  "http://",
			wantErr: true,
		},
		{
			name: "URL with javascript scheme",
			config: Config{
				TryHTTPSFirst: true,
			},
			rawURL:  "javascript:alert(1)",
			wantErr: true,
		},
		{
			name: "URL with mailto scheme",
			config: Config{
				TryHTTPSFirst: true,
			},
			rawURL:  "mailto:test@example.com",
			wantErr: true,
		},
		{
			name: "URL with spaces",
			config: Config{
				TryHTTPSFirst: true,
			},
			rawURL:   "  example.com  ",
			wantURLs: []string{"https://example.com", "http://example.com"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			checker := New(tt.config)

			urls, err := checker.prepareURLsToTry(tt.rawURL)
			be.Err(t, err, tt.wantErr)
			be.Equal(t, urls, tt.wantURLs)
		})
	}
}
func TestChecker_isAvailable(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		want       bool
	}{
		{
			name:       "200 OK",
			statusCode: http.StatusOK,
			want:       true,
		},
		{
			name:       "201 Created",
			statusCode: http.StatusCreated,
			want:       true,
		},
		{
			name:       "204 No Content",
			statusCode: http.StatusNoContent,
			want:       true,
		},
		{
			name:       "206 Partial Content",
			statusCode: http.StatusPartialContent,
			want:       true,
		},
		{
			name:       "299 custom 2xx",
			statusCode: 299,
			want:       true,
		},
		{
			name:       "100 Continue",
			statusCode: http.StatusContinue,
			want:       false,
		},
		{
			name:       "301 Moved Permanently",
			statusCode: http.StatusMovedPermanently,
			want:       false,
		},
		{
			name:       "302 Found",
			statusCode: http.StatusFound,
			want:       false,
		},
		{
			name:       "304 Not Modified",
			statusCode: http.StatusNotModified,
			want:       false,
		},
		{
			name:       "400 Bad Request",
			statusCode: http.StatusBadRequest,
			want:       false,
		},
		{
			name:       "404 Not Found",
			statusCode: http.StatusNotFound,
			want:       false,
		},
		{
			name:       "418 I'm a teapot",
			statusCode: http.StatusTeapot,
			want:       false,
		},
		{
			name:       "500 Internal Server Error",
			statusCode: http.StatusInternalServerError,
			want:       false,
		},
		{
			name:       "503 Service Unavailable",
			statusCode: http.StatusServiceUnavailable,
			want:       false,
		},
	}

	checker := New(Config{})

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := checker.isAvailable(tt.statusCode)
			be.Equal(t, got, tt.want)
		})
	}
}

func TestChecker_tryHEAD(t *testing.T) {
	tests := []struct {
		name           string
		serverHandler  http.HandlerFunc
		wantStatusCode int
		wantErr        any
	}{
		{
			name: "successful HEAD request",
			serverHandler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
			},
			wantStatusCode: http.StatusOK,
		},
		{
			name: "HEAD returns 404",
			serverHandler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusNotFound)
			},
			wantStatusCode: http.StatusNotFound,
		},
		{
			name: "HEAD returns 500",
			serverHandler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusInternalServerError)
			},
			wantStatusCode: http.StatusInternalServerError,
		},
		{
			name: "HEAD returns 301 redirect",
			serverHandler: func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Location", "/new-location")
				w.WriteHeader(http.StatusMovedPermanently)
			},
			wantErr: "network error:",
		},
		{
			name: "server timeout",
			serverHandler: func(w http.ResponseWriter, r *http.Request) {
				time.Sleep(200 * time.Millisecond) // Longer than test timeout
			},
			wantErr: "network error:",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create test server
			server := httptest.NewServer(tt.serverHandler)
			defer server.Close()

			// Create checker
			checker := New(Config{
				Timeout:   100 * time.Millisecond,
				UserAgent: "TestChecker",
			})

			// Run tryHEAD
			ctx := context.Background()
			statusCode, err := checker.tryHEAD(ctx, server.URL)
			be.Err(t, err, tt.wantErr)
			be.Equal(t, statusCode, tt.wantStatusCode)
		})
	}
}
