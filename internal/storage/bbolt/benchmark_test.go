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

const benchMaxBatchDelay = 10 * time.Millisecond

func BenchmarkStorage_Save(b *testing.B) {
	storLogger := slog.New(slog.DiscardHandler)

	tests := []struct {
		name string
		cfg  Config
	}{
		{
			"NoSync:No syncer:No saver:No",
			Config{
				NoSync: false,
			},
		},
		{
			"NoSync:Yes syncer:No saver:No",
			Config{
				NoSync: true,
			},
		},
		{
			"NoSync:Yes syncer:Yes saver:No",
			Config{
				NoSync:         true,
				MaxSyncDelay:   100 * time.Millisecond,
				MaxSyncPending: 1000,
			},
		},
		{
			"NoSync:No syncer:No saver:Yes",
			Config{
				NoSync:        false,
				MaxBatchDelay: benchMaxBatchDelay,
				MaxBatchSize:  runtime.GOMAXPROCS(0) * 2,
			},
		},
		// TODO
	}

	ctx := context.Background()

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
				defer storage.Close(ctx)

				b.ResetTimer()

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
				defer storage.Close(ctx)

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
	tests := []struct {
		name string
		cfg  Config
	}{
		{
			"queue10",
			Config{
				MaxBatchDelay: benchMaxBatchDelay,
				MaxBatchSize:  10,
			},
		},
		{
			"queue100",
			Config{
				MaxBatchDelay: benchMaxBatchDelay,
				MaxBatchSize:  100,
			},
		},
		{
			"queue1000",
			Config{
				MaxBatchDelay: benchMaxBatchDelay,
				MaxBatchSize:  1000,
			},
		},
		{
			"queue10000",
			Config{
				MaxBatchDelay: benchMaxBatchDelay,
				MaxBatchSize:  10000,
			},
		},
		// TODO
	}

	for _, tt := range tests {
		b.Run(tt.name, func(b *testing.B) {
			tmpFile := filepath.Join(b.TempDir(), "bench.db")

			tt.cfg.DataFile = tmpFile
			tt.cfg.Logger = slog.New(slog.DiscardHandler)

			ctx := context.Background()

			storage, err := Open(tt.cfg)
			if err != nil {
				b.Fatalf("open storage: %v", err)
				return
			}
			defer storage.Close(ctx)

			// Подготавливаем тестовые данные один раз
			links := []model.Link{
				{Name: "Test Link 1", URL: "https://example.com/1"},
				{Name: "Test Link 2", URL: "https://example.com/2"},
				{Name: "Test Link 3", URL: "https://example.com/3"},
			}

			ch := make(chan []model.Link)
			clients := tt.cfg.MaxBatchSize * 3
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

func BenchmarkStorage_LoadWithCache(b *testing.B) {
	tmpFile := filepath.Join(b.TempDir(), "bench.db")

	N := 100000

	links := []model.Link{
		{Name: "Test Link 1", URL: "https://example.com/1"},
		{Name: "Test Link 2", URL: "https://example.com/2"},
		{Name: "Test Link 3", URL: "https://example.com/3"},
	}

	ids := func() []uint64 {
		start := time.Now()

		batch := make([][]model.Link, 0, 2048)
		for range cap(batch) {
			batch = append(batch, links)
		}

		ctx := context.Background()

		storage, err := Open(Config{
			DataFile: tmpFile,
			Logger:   newTestLogger(),
			NoSync:   true,
		})
		be.Err(b, err, nil)

		allIDs := make([]uint64, 0, N)

		for i, n := 0, N/len(batch); i < n; i++ {
			curIDs, err := storage.SaveBatch(ctx, batch)
			be.Err(b, err, nil)
			allIDs = append(allIDs, curIDs...)
		}

		if n := N % len(batch); n > 0 {
			curIDs, err := storage.SaveBatch(ctx, batch[:n])
			be.Err(b, err, nil)
			allIDs = append(allIDs, curIDs...)
		}

		err = storage.Close(ctx)
		be.Err(b, err, nil)

		b.Logf("saved %d records in %v", len(allIDs), time.Since(start))

		return allIDs
	}()

	tests := []struct {
		name string
		cfg  Config
	}{
		{
			"0%",
			Config{
				CacheSize: 0,
			},
		},
		{
			"3%",
			Config{
				CacheSize: N / 33,
			},
		},
		{
			"10%",
			Config{
				CacheSize: N / 10,
			},
		},
		{
			"25%",
			Config{
				CacheSize: N / 4,
			},
		},
		{
			"50%",
			Config{
				CacheSize: N / 2,
			},
		},
		{
			"100%",
			Config{
				CacheSize: N,
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
			defer storage.Close(ctx)

			for i := 0; i < tt.cfg.CacheSize; i++ {
				j := rand.IntN(N)
				id := ids[j]
				_, err := storage.Load(ctx, id)
				if err != nil {
					b.Fatal(err)
				}
			}

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
