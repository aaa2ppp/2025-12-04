package config

import (
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/aaa2ppp/be"
)

func TestGetenv(t *testing.T) {
	t.Run("String", func(t *testing.T) {
		t.Run("present", func(t *testing.T) {
			t.Setenv("NAME", "Alice")
			ge := &Getenv{}
			v := ge.String("NAME", Required, "default")
			be.Equal(t, v, "Alice")
			be.Err(t, ge.Err(), false)
		})

		t.Run("absent, required", func(t *testing.T) {
			ge := &Getenv{}
			v := ge.String("MISSING", Required, "default")
			be.Equal(t, v, "") // zero value for string
			be.Err(t, ge.Err(), ErrEnvRequired)
			be.True(t, strings.Contains(ge.Err().Error(), "MISSING"))
		})

		t.Run("absent, not required", func(t *testing.T) {
			ge := &Getenv{}
			v := ge.String("MISSING", false, "default")
			be.Equal(t, v, "default")
			be.Err(t, ge.Err(), false)
		})

		t.Run("empty, required", func(t *testing.T) {
			t.Setenv("EMPTY", "")
			ge := &Getenv{}
			v := ge.String("EMPTY", Required, "default")
			be.Equal(t, v, "")
			be.Err(t, ge.Err(), ErrEnvRequired)
		})
	})

	t.Run("Strings", func(t *testing.T) {
		t.Run("present", func(t *testing.T) {
			t.Setenv("TAGS", "go test config")
			ge := &Getenv{}
			v := ge.Strings("TAGS", Required, []string{"default"})
			be.Equal(t, v, []string{"go", "test", "config"})
			be.Err(t, ge.Err(), false)
		})

		t.Run("absent, not required", func(t *testing.T) {
			ge := &Getenv{}
			defaultVal := []string{"fallback"}
			v := ge.Strings("MISSING", false, defaultVal)
			be.Equal(t, v, defaultVal)
			be.Err(t, ge.Err(), false)
		})
	})

	t.Run("Int", func(t *testing.T) {
		t.Run("valid", func(t *testing.T) {
			t.Setenv("PORT", "8080")
			ge := &Getenv{}
			v := ge.Int("PORT", Required, 80)
			be.Equal(t, v, 8080)
			be.Err(t, ge.Err(), false)
		})

		t.Run("invalid", func(t *testing.T) {
			t.Setenv("PORT", "not-a-number")
			ge := &Getenv{}
			v := ge.Int("PORT", Required, 80)
			be.Equal(t, v, 0) // zero value
			be.Err(t, ge.Err())
			be.True(t, strings.Contains(ge.Err().Error(), "PORT"))
			be.True(t, strings.Contains(ge.Err().Error(), "not-a-number"))
		})
	})

	t.Run("LogLevel", func(t *testing.T) {
		t.Run("valid", func(t *testing.T) {
			t.Setenv("LOG_LEVEL", "DEBUG")
			ge := &Getenv{}
			v := ge.LogLevel("LOG_LEVEL", Required, slog.LevelInfo)
			be.Equal(t, v, slog.LevelDebug)
			be.Err(t, ge.Err(), false)
		})

		t.Run("invalid", func(t *testing.T) {
			t.Setenv("LOG_LEVEL", "INVALID")
			ge := &Getenv{}
			v := ge.LogLevel("LOG_LEVEL", Required, slog.LevelInfo)
			be.Equal(t, v, slog.Level(0)) // zero value
			be.Err(t, ge.Err())
			be.True(t, strings.Contains(ge.Err().Error(), "LOG_LEVEL"))
			be.True(t, strings.Contains(ge.Err().Error(), "INVALID"))
		})
	})

	t.Run("Bool", func(t *testing.T) {
		t.Run("true values", func(t *testing.T) {
			for _, val := range []string{"true", "True", "TRUE", "yes", "1", "on"} {
				t.Run(val, func(t *testing.T) {
					t.Setenv("FLAG", val)
					ge := &Getenv{}
					v := ge.Bool("FLAG", Required, false)
					be.Equal(t, v, true)
					be.Err(t, ge.Err(), false)
				})
			}
		})

		t.Run("false values", func(t *testing.T) {
			for _, val := range []string{"false", "no", "0", "off"} {
				t.Run(val, func(t *testing.T) {
					t.Setenv("FLAG", val)
					ge := &Getenv{}
					v := ge.Bool("FLAG", Required, true)
					be.Equal(t, v, false)
					be.Err(t, ge.Err(), false)
				})
			}
		})

		t.Run("invalid", func(t *testing.T) {
			t.Setenv("FLAG", "maybe")
			ge := &Getenv{}
			v := ge.Bool("FLAG", Required, false)
			be.Equal(t, v, false) // default on error
			be.Err(t, ge.Err())
			be.True(t, strings.Contains(ge.Err().Error(), "FLAG"))
			be.True(t, strings.Contains(ge.Err().Error(), "maybe"))
		})
	})

	t.Run("Duration", func(t *testing.T) {
		t.Run("valid", func(t *testing.T) {
			t.Setenv("TIMEOUT", "5s")
			ge := &Getenv{}
			v := ge.Duration("TIMEOUT", Required, 1*time.Second)
			be.Equal(t, v, 5*time.Second)
			be.Err(t, ge.Err(), false)
		})

		t.Run("invalid", func(t *testing.T) {
			t.Setenv("TIMEOUT", "five seconds")
			ge := &Getenv{}
			v := ge.Duration("TIMEOUT", Required, 1*time.Second)
			be.Equal(t, v, time.Duration(0))
			be.Err(t, ge.Err())
			be.True(t, strings.Contains(ge.Err().Error(), "TIMEOUT"))
			be.True(t, strings.Contains(ge.Err().Error(), "five seconds"))
		})
	})

	t.Run("URL", func(t *testing.T) {
		t.Run("valid", func(t *testing.T) {
			t.Setenv("API_URL", "https://api.example.com")
			ge := &Getenv{}
			v := ge.URL("API_URL", Required, "http://localhost")
			be.Equal(t, v, "https://api.example.com")
			be.Err(t, ge.Err(), false)
		})

		t.Run("invalid", func(t *testing.T) {
			t.Setenv("API_URL", "://invalid-url")
			ge := &Getenv{}
			v := ge.URL("API_URL", Required, "http://localhost")
			be.Equal(t, v, "")
			be.Err(t, ge.Err())
			be.True(t, strings.Contains(ge.Err().Error(), "API_URL"))
			be.True(t, strings.Contains(ge.Err().Error(), "invalid-url"))
		})
	})

	t.Run("multiple errors", func(t *testing.T) {
		t.Setenv("PORT", "not-a-number")
		t.Setenv("TIMEOUT", "five seconds")
		ge := &Getenv{}
		ge.Int("PORT", Required, 80)
		ge.Duration("TIMEOUT", Required, 5*time.Second)
		err := ge.Err()
		be.Err(t, err)
		be.True(t, strings.Contains(err.Error(), "PORT"))
		be.True(t, strings.Contains(err.Error(), "TIMEOUT"))
	})
}
