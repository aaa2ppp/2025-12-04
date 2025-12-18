package bbolt

import (
	"log/slog"

	"go.etcd.io/bbolt"
	berrors "go.etcd.io/bbolt/errors"
)

type Bucket interface {
	Put(key, data []byte) error
	Get(key []byte) []byte
}

type Tx interface {
	Bucket(name []byte) Bucket
}

type DB interface {
	Update(func(tx Tx) error) error
	View(func(tx Tx) error) error
	Sync() error
	Close() error
}

type bboltTx struct {
	tx *bbolt.Tx
}

func (bb bboltTx) Bucket(name []byte) Bucket {
	return bb.tx.Bucket(name)
}

type bboltDB struct {
	db *bbolt.DB
}

func (bb bboltDB) Update(fn func(Tx) error) error {
	return bb.db.Update(func(tx *bbolt.Tx) error {
		return fn(bboltTx{tx})
	})
}

func (bb bboltDB) View(fn func(Tx) error) error {
	return bb.db.View(func(tx *bbolt.Tx) error {
		return fn(bboltTx{tx})
	})
}

func (bb bboltDB) Sync() error {
	return bb.db.Sync()
}

func (bb bboltDB) Close() error {
	if bb.db.NoSync {
		// Гарантируем, что после выхода из Close все изменения будут сохранены на диске.
		// После сброса флага NoSync последующие транзакции записи будут выполняться fdatasync.
		bb.db.NoSync = false

		// Гарантируем, что после сброса флага NoSync будет по крайней мере одна такая транзакция.
		err := bb.db.Update(func(tx *bbolt.Tx) error { return nil })
		if err != nil && err != berrors.ErrDatabaseNotOpen {
			slog.Warn("final sync transaction failed", "error", err)
		}
	}

	// db.Close ожидает завершения всех незавершенных транзакций (если такие есть).
	// В bbolt одновременно может существовать только одна транзакция записи.
	// Это означает, что транзакции записи, которые ожидает db.Close, выполняются после
	// нашей пустой транзакции и, следовательно, после сброса флага NoSync и будут
	// выполнять fdatasync.
	if err := bb.db.Close(); err != nil {
		return err
	}

	return nil
}
