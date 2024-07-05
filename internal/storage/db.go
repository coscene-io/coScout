package storage

import (
	"errors"
	"time"

	log "github.com/sirupsen/logrus"
	"go.etcd.io/bbolt"
)

type BoltDB struct {
	db *bbolt.DB
}

func NewBoltDB(filepath string) Storage {
	db, err := bbolt.Open(filepath, 0600, &bbolt.Options{Timeout: 3 * time.Second})
	if err != nil {
		log.Fatal(err)
	}

	return &BoltDB{db: db}
}

func (bb *BoltDB) Put(bucket, key, value []byte) error {
	return bb.db.Update(func(tx *bbolt.Tx) error {
		createdBucket, err := tx.CreateBucketIfNotExists(bucket)
		if err != nil {
			log.Errorf("create bucket: %s", err)
			return errors.New("create bucket")
		}

		err = createdBucket.Put(key, value)
		if err != nil {
			log.Errorf("put key: %s", err)
			return errors.New("put key")
		}
		return err
	})
}

func (bb *BoltDB) Get(bucket, key []byte) ([]byte, error) {
	var value []byte
	err := bb.db.View(func(tx *bbolt.Tx) error {
		b := tx.Bucket(bucket)
		if b == nil {
			return nil
		}

		value = b.Get(key)
		if value == nil {
			value = []byte{}
		}
		return nil
	})
	return value, err
}

func (bb *BoltDB) Delete(bucket, key []byte) error {
	return bb.db.Update(func(tx *bbolt.Tx) error {
		b := tx.Bucket(bucket)
		if b == nil {
			return nil
		}

		err := b.Delete(key)
		if err != nil {
			return errors.New("delete key")
		}
		return nil
	})
}

func (bb *BoltDB) Close() error {
	return bb.db.Close()
}
