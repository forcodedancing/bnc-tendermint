//+build rocksdb

package db

import (
	"bytes"
	"fmt"
	"path/filepath"

	"github.com/tecbot/gorocksdb"
)

func init() {
	dbCreator := func(name string, dir string, opt interface{}) (DB, error) {
		if o, ok := opt.(*Options); ok {
			return NewRocksDB(name, dir, o)
		} else {
			return NewRocksDB(name, dir, nil)
		}
	}
	registerDBCreator(RocksDBBackend, dbCreator, true)
}

var _ DB = (*RocksDB)(nil)

type RocksDB struct {
	db     *gorocksdb.DB
	ro     *gorocksdb.ReadOptions
	wo     *gorocksdb.WriteOptions
	woSync *gorocksdb.WriteOptions
}

func NewRocksDB(name string, dir string, option *Options) (*RocksDB, error) {
	dbPath := filepath.Join(dir, name+".db")

	opts := gorocksdb.NewDefaultOptions()
	if option != nil {
		bbto := gorocksdb.NewDefaultBlockBasedTableOptions()
		if option.BlockCacheSize > 0 {
			bbto.SetBlockCache(gorocksdb.NewLRUCache(option.BlockCacheSize))
		}

		if option.FilterBitsPerKey > 0 {
			filter := gorocksdb.NewBloomFilter(option.FilterBitsPerKey)
			bbto.SetFilterPolicy(filter)
		}

		bbto.SetBlockSize(16 * 1024)
		bbto.SetCacheIndexAndFilterBlocks(true)
		bbto.SetPinL0FilterAndIndexBlocksInCache(true)

		opts.SetBlockBasedTableFactory(bbto)
		//opts.SetCompression(gorocksdb.LZ4Compression)

		if option.WriteBufferSize > 0 {
			opts.SetWriteBufferSize(option.WriteBufferSize)
		}
	}

	opts.SetMaxBackgroundCompactions(4)
	opts.SetMaxBackgroundFlushes(2)

	opts.SetCreateIfMissing(true)
	db, err := gorocksdb.OpenDb(opts, dbPath)
	if err != nil {
		return nil, err
	}
	ro := gorocksdb.NewDefaultReadOptions()
	wo := gorocksdb.NewDefaultWriteOptions()
	woSync := gorocksdb.NewDefaultWriteOptions()
	woSync.SetSync(true)
	database := &RocksDB{
		db:     db,
		ro:     ro,
		wo:     wo,
		woSync: woSync,
	}
	return database, nil
}

// Implements DB.
func (db *RocksDB) Get(key []byte) []byte {
	key = nonNilBytes(key)
	res, err := db.db.GetBytes(db.ro, key)
	if err != nil {
		panic(err)
	}
	return res
}

// Implements DB.
func (db *RocksDB) Has(key []byte) bool {
	return db.Get(key) == nil
}

// Implements DB.
func (db *RocksDB) Set(key []byte, value []byte) {
	key = nonNilBytes(key)
	value = nonNilBytes(value)
	err := db.db.Put(db.wo, key, value)
	if err != nil {
		panic(err)
	}
}

// Implements DB.
func (db *RocksDB) SetSync(key []byte, value []byte) {
	key = nonNilBytes(key)
	value = nonNilBytes(value)
	err := db.db.Put(db.woSync, key, value)
	if err != nil {
		panic(err)
	}
}

// Implements DB.
func (db *RocksDB) Delete(key []byte) {
	key = nonNilBytes(key)
	err := db.db.Delete(db.wo, key)
	if err != nil {
		panic(err)
	}
}

// Implements DB.
func (db *RocksDB) DeleteSync(key []byte) {
	key = nonNilBytes(key)
	err := db.db.Delete(db.woSync, key)
	if err != nil {
		panic(err)
	}
}

func (db *RocksDB) DB() *gorocksdb.DB {
	return db.db
}

// Implements DB.
func (db *RocksDB) Close() {
	db.db.Close()
	db.ro.Destroy()
	db.wo.Destroy()
	db.woSync.Destroy()
}

// Implements DB.
func (db *RocksDB) Print() {
	itr := db.Iterator(nil, nil)
	defer itr.Close()
	for ; itr.Valid(); itr.Next() {
		key := itr.Key()
		value := itr.Value()
		fmt.Printf("[%X]:\t[%X]\n", key, value)
	}
}

// Implements DB.
func (db *RocksDB) Stats() map[string]string {
	keys := []string{
		"rocksdb.size-all-mem-tables",
		"rocksdb.cur-size-all-mem-tables",
		"rocksdb.estimate-num-keys",
		"rocksdb.block-cache-usage",
		"rocksdb.block-cache-capacity",
		"rocksdb.block-cache-pinned-usage",
		"rocksdb.sstables",
		"rocksdb.estimate-table-readers-mem",
		"rocksdb.stats",
	}

	stats := make(map[string]string, len(keys))
	for _, key := range keys {
		str := db.db.GetProperty(key)
		stats[key] = str
	}
	return stats
}

//----------------------------------------
// Batch

// Implements DB.
func (db *RocksDB) NewBatch() Batch {
	batch := gorocksdb.NewWriteBatch()
	return &rocksDBBatch{db, batch}
}

type rocksDBBatch struct {
	db    *RocksDB
	batch *gorocksdb.WriteBatch
}

// Implements Batch.
func (mBatch *rocksDBBatch) Set(key, value []byte) {
	mBatch.batch.Put(key, value)
}

// Implements Batch.
func (mBatch *rocksDBBatch) Delete(key []byte) {
	mBatch.batch.Delete(key)
}

// Implements Batch.
func (mBatch *rocksDBBatch) Write() {
	err := mBatch.db.db.Write(mBatch.db.wo, mBatch.batch)
	if err != nil {
		panic(err)
	}
}

// Implements Batch.
func (mBatch *rocksDBBatch) WriteSync() {
	err := mBatch.db.db.Write(mBatch.db.woSync, mBatch.batch)
	if err != nil {
		panic(err)
	}
}

// Implements Batch.
func (mBatch *rocksDBBatch) Close() {
	mBatch.batch.Destroy()
}

//----------------------------------------
// Iterator
// NOTE This is almost identical to db/go_level_db.Iterator
// Before creating a third version, refactor.

func (db *RocksDB) Iterator(start, end []byte) Iterator {
	itr := db.db.NewIterator(db.ro)
	return newRocksDBIterator(itr, start, end, false)
}

func (db *RocksDB) ReverseIterator(start, end []byte) Iterator {
	itr := db.db.NewIterator(db.ro)
	return newRocksDBIterator(itr, start, end, true)
}

var _ Iterator = (*rocksDBIterator)(nil)

type rocksDBIterator struct {
	source     *gorocksdb.Iterator
	start, end []byte
	isReverse  bool
	isInvalid  bool
}

func newRocksDBIterator(source *gorocksdb.Iterator, start, end []byte, isReverse bool) *rocksDBIterator {
	if isReverse {
		if end == nil {
			source.SeekToLast()
		} else {
			source.Seek(end)
			if source.Valid() {
				var eoakey []byte = nil
				if source.Key() != nil {
					eoakey = source.Key().Data()
				}

				if bytes.Compare(end, eoakey) <= 0 {
					source.Prev()
				}
			} else {
				source.SeekToLast()
			}
		}
	} else {
		if start == nil {
			source.SeekToFirst()
		} else {
			source.Seek(start)
		}
	}
	return &rocksDBIterator{
		source:    source,
		start:     start,
		end:       end,
		isReverse: isReverse,
		isInvalid: false,
	}
}

func (itr rocksDBIterator) Domain() ([]byte, []byte) {
	return itr.start, itr.end
}

func (itr rocksDBIterator) Valid() bool {

	// Once invalid, forever invalid.
	if itr.isInvalid {
		return false
	}

	// Panic on DB error.  No way to recover.
	itr.assertNoError()

	// If source is invalid, invalid.
	if !itr.source.Valid() {
		itr.isInvalid = true
		return false
	}

	// If key is end or past it, invalid.
	var start = itr.start
	var end = itr.end
	var key []byte = nil
	if itr.source.Key() != nil {
		key = itr.source.Key().Data()
	}

	if itr.isReverse {
		if start != nil && bytes.Compare(key, start) < 0 {
			itr.isInvalid = true
			return false
		}
	} else {
		if end != nil && bytes.Compare(end, key) <= 0 {
			itr.isInvalid = true
			return false
		}
	}

	// It's valid.
	return true
}

func (itr rocksDBIterator) Key() []byte {
	itr.assertNoError()
	itr.assertIsValid()

	return getSliceBytes(itr.source.Key())
}

func (itr rocksDBIterator) Value() []byte {
	itr.assertNoError()
	itr.assertIsValid()

	return getSliceBytes(itr.source.Value())
}

func (itr rocksDBIterator) Next() {
	itr.assertNoError()
	itr.assertIsValid()
	if itr.isReverse {
		itr.source.Prev()
	} else {
		itr.source.Next()
	}
}

func (itr rocksDBIterator) Close() {
	itr.source.Close()
}

func (itr rocksDBIterator) assertNoError() {
	if err := itr.source.Err(); err != nil {
		panic(err)
	}
}

func (itr rocksDBIterator) assertIsValid() {
	if !itr.Valid() {
		panic("rocksDBIterator is invalid")
	}
}

func getSliceBytes(slice *gorocksdb.Slice) []byte {
	if slice == nil {
		return nil
	}
	tmpData := make([]byte, len(slice.Data()))
	copy(tmpData, slice.Data())
	return tmpData
}
