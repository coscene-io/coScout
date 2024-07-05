package storage

type Storage interface {
	Put(bucket, key, value []byte) error
	Get(bucket, key []byte) ([]byte, error)
	Delete(bucket, key []byte) error
	Close() error
}
