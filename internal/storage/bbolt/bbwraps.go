package bbolt

import "go.etcd.io/bbolt"

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
