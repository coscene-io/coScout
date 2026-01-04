// Copyright 2025 coScene
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package storage

import (
	"time"

	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
	"go.etcd.io/bbolt"
)

type BoltDB struct {
	db *bbolt.DB
}

func NewBoltDB(filepath string) Storage {
	db, err := bbolt.Open(filepath, 0600, &bbolt.Options{Timeout: 3 * time.Second})
	if err != nil {
		log.Fatal("open local boltdb ", filepath, ", err: ", err)
	}

	return &BoltDB{db: db}
}

func (bb *BoltDB) Iter(bucket []byte, fn func(key []byte, value []byte) error) error {
	return bb.db.View(func(tx *bbolt.Tx) error {
		b := tx.Bucket(bucket)
		if b == nil {
			return nil
		}

		return b.ForEach(func(key, value []byte) error {
			if err := fn(key, value); err != nil {
				return errors.Wrap(err, "iterate bucket")
			}
			return nil
		})
	})
}

func (bb *BoltDB) Put(bucket, key, value []byte) error {
	return bb.db.Update(func(tx *bbolt.Tx) error {
		createdBucket, err := tx.CreateBucketIfNotExists(bucket)
		if err != nil {
			log.Errorf("create bucket: %s", err)
			return errors.Wrap(err, "create bucket")
		}

		err = createdBucket.Put(key, value)
		if err != nil {
			log.Errorf("put key: %s", err)
			return errors.Wrap(err, "put key")
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
			return errors.Wrap(err, "delete key")
		}
		return nil
	})
}

func (bb *BoltDB) Close() error {
	return bb.db.Close()
}
