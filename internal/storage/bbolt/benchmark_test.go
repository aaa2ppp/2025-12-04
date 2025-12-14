package bbolt

import (
	"context"
	"log/slog"
	"math/rand/v2"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"link-checker/internal/logger"
	"link-checker/internal/model"

	"github.com/aaa2ppp/be"
)

const benchMaxSaveDelay = 5 * time.Millisecond

func BenchmarkStorage_Save(b *testing.B) {
	tests := []struct {
		name string
		cfg  Config
	}{
		{
			"NoSync:No syncer:No saver:No",
			Config{
				NoSync:       false,
				MaxCache:     -1,
				MaxSyncDelay: -1,
				MaxSaveDelay: -1,
			},
		},
		{
			"NoSync:Yes syncer:No saver:No",
			Config{
				NoSync:       true,
				MaxCache:     -1,
				MaxSyncDelay: -1,
				MaxSaveDelay: -1,
			},
		},
		{
			"NoSync:Yes syncer:Yes saver:No",
			Config{
				NoSync:       true,
				MaxCache:     -1,
				MaxSyncDelay: 0,
				MaxSaveDelay: -1,
			},
		},
		{
			"NoSync:No syncer:No saver:Yes",
			Config{
				Timeout:          1 * time.Second, // передается bbolt.DB
				NoSync:           false,           // передается bbolt.DB
				MaxCache:         -1,
				MaxSyncDelay:     -1,
				MaxSaveDelay:     benchMaxSaveDelay,
				MaxSaveQueueSize: runtime.GOMAXPROCS(0) * 2, // чтобы зависело только от delay
			},
		},
		// TODO
	}

	storLogger := slog.New(slog.DiscardHandler)
	ctx := logger.Context(context.Background(), storLogger)

	links := []model.Link{
		{Name: "Test Link 1", URL: "https://example.com/1"},
		{Name: "Test Link 2", URL: "https://example.com/2"},
		{Name: "Test Link 3", URL: "https://example.com/3"},
	}

	b.Run("sequential", func(b *testing.B) {
		for _, tt := range tests {
			b.Run(tt.name, func(b *testing.B) {
				tmpFile := filepath.Join(b.TempDir(), "bench.db")

				tt.cfg.DataFile = tmpFile
				tt.cfg.Logger = storLogger

				storage, err := Open(tt.cfg)
				if err != nil {
					b.Fatalf("open: %v", err)
				}
				defer storage.Close()

				for i := 0; i < b.N; i++ {
					_, err := storage.Save(ctx, links)
					if err != nil {
						b.Fatalf("save: %v", err)
					}
				}
			})
		}
	})

	b.Run("parallel", func(b *testing.B) {
		for _, tt := range tests {
			b.Run(tt.name, func(b *testing.B) {
				tmpFile := filepath.Join(b.TempDir(), "bench.db")

				tt.cfg.DataFile = tmpFile
				tt.cfg.Logger = storLogger

				storage, err := Open(tt.cfg)
				if err != nil {
					b.Fatalf("open: %v", err)
				}
				defer storage.Close()

				ctx := logger.Context(context.Background(), storLogger)

				b.ResetTimer()

				b.RunParallel(func(pb *testing.PB) {
					for pb.Next() {
						_, err := storage.Save(ctx, links)
						if err != nil {
							b.Fatalf("save: %v", err)
						}
					}
				})
			})
		}
	})
}

func startClientPool(ctx context.Context, stor *Storage, in <-chan []model.Link, out chan<- uint64, n int) <-chan error {
	done := make(chan error, 1)

	go func() {
		defer close(done)

		ctx, cancel := context.WithCancel(ctx)
		defer cancel()

		var (
			firstErr atomic.Value
			wg       sync.WaitGroup
		)

		wg.Add(n)
		for range n {
			go func() {
				defer wg.Done()
				if err := clientLoop(ctx, stor, in, out); err != nil {
					firstErr.CompareAndSwap(nil, err)
					cancel()
				}
			}()
		}

		wg.Wait()

		if err := firstErr.Load(); err != nil {
			done <- err.(error)
		} else {
			done <- nil
		}
	}()

	return done
}

func clientLoop(ctx context.Context, stor *Storage, in <-chan []model.Link, out chan<- uint64) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case links, ok := <-in:
			if !ok {
				return nil
			}
			if id, err := stor.Save(ctx, links); err != nil {
				return err
			} else if out != nil {
				out <- id
			}
		}
	}
}

func BenchmarkStorage_ConcurentSaveWithSaver(b *testing.B) {
	storLogger := slog.New(slog.DiscardHandler)

	tests := []struct {
		name string
		cfg  Config
	}{
		{
			"queue10",
			Config{
				Timeout:          1 * time.Second, // передается bbolt.DB
				NoSync:           false,           // передается bbolt.DB
				MaxCache:         -1,
				MaxSyncDelay:     -1,
				MaxSaveDelay:     benchMaxSaveDelay,
				MaxSaveQueueSize: 10,
			},
		},
		{
			"queue100",
			Config{
				Timeout:          1 * time.Second, // передается bbolt.DB
				NoSync:           false,           // передается bbolt.DB
				MaxCache:         -1,
				MaxSyncDelay:     -1,
				MaxSaveDelay:     benchMaxSaveDelay,
				MaxSaveQueueSize: 100,
			},
		},
		{
			"queue1000",
			Config{
				Timeout:          1 * time.Second, // передается bbolt.DB
				NoSync:           false,           // передается bbolt.DB
				MaxCache:         -1,
				MaxSyncDelay:     -1,
				MaxSaveDelay:     benchMaxSaveDelay,
				MaxSaveQueueSize: 1000,
			},
		},
		{
			"queue10000",
			Config{
				Timeout:          1 * time.Second, // передается bbolt.DB
				NoSync:           false,           // передается bbolt.DB
				MaxCache:         -1,
				MaxSyncDelay:     -1,
				MaxSaveDelay:     benchMaxSaveDelay,
				MaxSaveQueueSize: 10000,
			},
		},
		// TODO
	}

	for _, tt := range tests {
		b.Run(tt.name, func(b *testing.B) {
			tmpFile := filepath.Join(b.TempDir(), "bench.db")

			tt.cfg.DataFile = tmpFile
			tt.cfg.Logger = storLogger

			storage, err := Open(tt.cfg)
			if err != nil {
				b.Fatalf("open storage: %v", err)
				return
			}
			defer storage.Close()

			ctx := logger.Context(context.Background(), storLogger)

			// ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			// defer cancel()

			// Подготавливаем тестовые данные один раз
			links := []model.Link{
				{Name: "Test Link 1", URL: "https://example.com/1"},
				{Name: "Test Link 2", URL: "https://example.com/2"},
				{Name: "Test Link 3", URL: "https://example.com/3"},
			}

			ch := make(chan []model.Link)
			clients := tt.cfg.MaxSaveQueueSize * 3
			done := startClientPool(ctx, storage, ch, nil, clients)

			b.ResetTimer()

			for i := 0; i < b.N; i++ {
				select {
				case ch <- links:
				case err := <-done:
					b.Fatalf("pool: %v", err)
					return
				}
			}

			close(ch)

			for {
				select {
				case err := <-done:
					if err != nil {
						b.Fatalf("done: %v", err)
					}
					return
				case <-time.After(10 * time.Millisecond):
				}
			}
		})
	}
}

func BenchmarkStorage_Load(b *testing.B) {
	os.MkdirAll("./tmp", 0777)
	tmpFile := filepath.Join("./tmp/bench.db")

	N := 100000

	links := []model.Link{
		{Name: "Test Link 1", URL: "https://example.com/1"},
		{Name: "Test Link 2", URL: "https://example.com/2"},
		{Name: "Test Link 3", URL: "https://example.com/3"},
	}

	var ids []uint64
	{
		start := time.Now()

		cfg := Config{
			DataFile: tmpFile,
			Logger:   newTestLogger(),
			NoSync:   false,
		}

		storage, err := Open(cfg)
		be.Err(b, err, nil)

		batch := make([][]model.Link, 0, N)
		for range N {
			batch = append(batch, links)
		}

		ids, err = storage.SaveBatch(context.Background(), batch)
		be.Err(b, err, nil)

		err = storage.Close()
		be.Err(b, err, nil)

		b.Logf("saved %d records in %v", len(ids), time.Since(start))
	}

	tests := []struct {
		name string
		cfg  Config
	}{
		{
			"0%",
			Config{
				MaxCache: -1,
			},
		},
		{
			"3%",
			Config{
				MaxCache: N / 33,
			},
		},
		{
			"10%",
			Config{
				MaxCache: N / 10,
			},
		},
		{
			"25%",
			Config{
				MaxCache: N / 4,
			},
		},
		{
			"50%",
			Config{
				MaxCache: N / 2,
			},
		},
	}

	for _, tt := range tests {
		b.Run(tt.name, func(b *testing.B) {
			if _, err := os.Stat(tmpFile); err != nil {
				b.Fatal(err)
			}
			tt.cfg.DataFile = tmpFile
			tt.cfg.Logger = slog.New(slog.DiscardHandler)
			ctx := logger.Context(context.Background(), tt.cfg.Logger)

			storage, err := Open(tt.cfg)
			be.Err(b, err, nil)
			defer storage.Close()

			b.ResetTimer()

			for i := 0; i < b.N; i++ {
				j := rand.IntN(N)
				id := ids[j]
				_, err := storage.Load(ctx, id)
				if err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
