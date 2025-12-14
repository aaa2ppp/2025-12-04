package bbolt

import (
	"fmt"
	"log/slog"
	"os"
)

func panicToError(fn func()) (err error) {
	defer func() {
		if p := recover(); p != nil {
			err = fmt.Errorf("panic: %v", p)
		}
	}()
	fn()
	return nil
}

func newTestLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelWarn,
	}))
}

// Mock интерфейсов для тестов
type mockBucket struct {
	putFunc func(key, data []byte) error
	getFunc func(key []byte) []byte
}

func (m *mockBucket) Put(key, data []byte) error {
	if m.putFunc != nil {
		return m.putFunc(key, data)
	}
	return nil
}

func (m *mockBucket) Get(key []byte) []byte {
	if m.getFunc != nil {
		return m.getFunc(key)
	}
	return nil
}

type mockTx struct {
	bucketFunc func(name []byte) Bucket
}

func (m *mockTx) Bucket(name []byte) Bucket {
	if m.bucketFunc != nil {
		return m.bucketFunc(name)
	}
	return &mockBucket{}
}

type mockDB struct {
	updateFunc func(fn func(Tx) error) error
	viewFunc   func(fn func(Tx) error) error
	syncFunc   func() error
}

func (m *mockDB) Update(fn func(Tx) error) error {
	if m.updateFunc != nil {
		return m.updateFunc(fn)
	}
	return nil
}

func (m *mockDB) View(fn func(Tx) error) error {
	if m.viewFunc != nil {
		return m.viewFunc(fn)
	}
	return nil
}

func (m *mockDB) Sync() error {
	if m.syncFunc != nil {
		return m.syncFunc()
	}
	return nil
}
