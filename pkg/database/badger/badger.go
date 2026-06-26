// Copyright (c) 2026 @3899. All rights reserved.
// Use of this source code is governed by a MIT-style license that can be found in the LICENSE file.

package badger

import (
	"context"
	"errors"
	"fmt"
	"log"
	"math/rand"
	"strconv"
	"strings"
	"time"

	"github.com/dgraph-io/badger/v4"
)

type Badger struct {
	path string
	db   *badger.DB
}

func New(path string) (*Badger, error) {
	return NewWithOptions(path, 5, 1*time.Second, false)
}

func NewWithOptions(path string, maxRetries int, retryDelay time.Duration, silent bool) (*Badger, error) {
	var opts = badger.DefaultOptions(path).WithLoggingLevel(badger.WARNING)
	// .WithSyncWrites(false)
	var db *badger.DB
	var err error

	if maxRetries <= 0 {
		maxRetries = 1
	}

	for i := 0; i < maxRetries; i++ {
		db, err = badger.Open(opts)
		if err == nil {
			break
		}

		errStr := err.Error()
		isLockError := strings.Contains(errStr, "lock") ||
			strings.Contains(errStr, "resource temporarily unavailable") ||
			strings.Contains(errStr, "process cannot access") ||
			strings.Contains(errStr, "temporarily unavailable")

		if isLockError && i < maxRetries-1 {
			// 指数退避 + 随机抖动，避免重试风暴
			backoff := retryDelay * time.Duration(1<<uint(i))
			if backoff <= 0 {
				backoff = retryDelay
			}
			jitter := time.Duration(rand.Int63n(int64(500 * time.Millisecond)))
			wait := backoff + jitter
			if !silent {
				log.Printf("[badger] 无法获取数据库目录锁 %s (第 %d/%d 次尝试)，将在 %v 后重试... 错误原因: %v\n", path, i+1, maxRetries, wait, err)
			}
			time.Sleep(wait)
			continue
		}
		break
	}

	if err != nil {
		return nil, fmt.Errorf("failed to open badger db: %w", err)
	}

	b := &Badger{
		path: path,
		db:   db,
	}
	// 同步执行 Value Log GC，避免 Close() 后 goroutine 竞态
	if err := db.RunValueLogGC(0.5); err != nil {
		if !errors.Is(err, badger.ErrNoRewrite) {
			log.Printf("[badger] RunValueLogGC: %s\n", err)
		}
	}
	return b, nil
}

func (b *Badger) Close(ctx context.Context) error {
	return b.db.Close()
}

func (b *Badger) Set(ctx context.Context, key, value string, ttl ...time.Duration) error {
	return b.db.Update(func(txn *badger.Txn) error {
		entry := badger.NewEntry([]byte(key), []byte(value))
		if len(ttl) > 0 && ttl[0] > 0 {
			entry.WithTTL(ttl[0])
		}
		return txn.SetEntry(entry)
	})
}

func (b *Badger) Get(ctx context.Context, key string) (string, error) {
	var resp string
	if err := b.db.View(func(txn *badger.Txn) error {
		item, err := txn.Get([]byte(key))
		if err != nil {
			return err
		}
		return item.Value(func(val []byte) error {
			resp = string(val)
			return nil
		})
	}); err != nil {
		return "", err
	}
	return resp, nil
}

func (b *Badger) Exists(ctx context.Context, key string) (bool, error) {
	_, err := b.Get(ctx, key)
	if err != nil {
		if errors.Is(err, badger.ErrKeyNotFound) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// Increment 实现类似于redis中Incr命令
// TODO:
// 1.目前badger得过期时间是使用本地时区时间,因此上游设置时间同样也需要使用本地时区时间,不然会造成不符合预期结果。
// 2.由于badger支持有限,因此在设置过期时间后,更新操作需要每次自己计算过期时间,如果不指定过期时间则相当移除了过期时间。
// 3.是否存在并发问题有待商榷
func (b *Badger) Increment(ctx context.Context, key string, value int64, ttl ...time.Duration) (int64, error) {
	var oldValue int64
	err := b.db.Update(func(txn *badger.Txn) error {
		item, err := txn.Get([]byte(key))
		if errors.Is(err, badger.ErrKeyNotFound) {
			// continue
		} else if err != nil {
			return err
		} else {
			v, err := item.ValueCopy(nil)
			if err != nil {
				return fmt.Errorf("ValueCopy: %w", err)
			}
			oldValue, err = strconv.ParseInt(string(v), 10, 64)
			if err != nil {
				return fmt.Errorf("ParseInt: %w", err)
			}
			value += oldValue
		}

		var entry = badger.NewEntry([]byte(key), []byte(fmt.Sprintf("%v", value)))
		if len(ttl) > 0 && ttl[0] > 0 {
			entry.WithTTL(ttl[0])
		}
		return txn.SetEntry(entry)
	})
	return oldValue, err
}

func (b *Badger) Del(ctx context.Context, key string) error {
	return b.db.Update(func(txn *badger.Txn) error {
		return txn.Delete([]byte(key))
	})
}
