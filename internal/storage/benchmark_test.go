package storage

import (
	"context"
	"math/rand/v2"
	"path/filepath"
	"testing"

	"link-checker/internal/model"
)

func BenchmarkStorage_Save(b *testing.B) {
	tmpFile := filepath.Join(b.TempDir(), "bench.db")
	storage, err := Open(Config{
		MaxCache: 1000,
		DataFile: tmpFile,
	})
	if err != nil {
		b.Fail()
	}
	defer storage.Close()

	ctx := context.Background()

	// Подготавливаем тестовые данные один раз
	links := []model.Link{
		{Name: "Test Link 1", URL: "https://example.com/1"},
		{Name: "Test Link 2", URL: "https://example.com/2"},
		{Name: "Test Link 3", URL: "https://example.com/3"},
	}

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_, err := storage.Save(ctx, links)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkStorage_ConcurrentSave(b *testing.B) {
	tmpFile := filepath.Join(b.TempDir(), "bench.db")
	storage, err := Open(Config{
		MaxCache: 10000,
		DataFile: tmpFile,
	})
	if err != nil {
		b.Fatal(err)
	}
	defer storage.Close()

	ctx := context.Background()

	links := []model.Link{
		{Name: "Test Link 1", URL: "https://example.com/1"},
		{Name: "Test Link 2", URL: "https://example.com/2"},
		{Name: "Test Link 3", URL: "https://example.com/3"},
	}

	b.ResetTimer()

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_, err := storage.Save(ctx, links)
			if err != nil {
				b.Fatal(err)
			}
		}
	})
}

func BenchmarkStorage_Load(b *testing.B) {
	N := 1000

	tmpFile := filepath.Join(b.TempDir(), "bench.db")
	storage, err := Open(Config{
		MaxCache: N / 33, // 3% попадания в кеш
		DataFile: tmpFile,
	})
	if err != nil {
		b.Fail()
	}
	defer storage.Close()

	ctx := context.Background()

	links := []model.Link{
		{Name: "Test Link 1", URL: "https://example.com/1"},
		{Name: "Test Link 2", URL: "https://example.com/2"},
		{Name: "Test Link 3", URL: "https://example.com/3"},
	}

	ids := make([]uint64, 0, N)

	for range N {
		id, err := storage.Save(ctx, links)
		if err != nil {
			b.Fatal(err)
		}
		ids = append(ids, id)
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
}
