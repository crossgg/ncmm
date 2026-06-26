// Copyright (c) 2026 @3899. All rights reserved.
// Use of this source code is governed by a MIT-style license that can be found in the LICENSE file.

package database

import (
	"context"
	"fmt"
	"time"

	"github.com/3899/ncmm/pkg/database/badger"
)

type Database interface {
	Get(ctx context.Context, key string) (string, error)
	Set(ctx context.Context, key string, value string, ttl ...time.Duration) error
	Exists(ctx context.Context, key string) (bool, error)
	Increment(ctx context.Context, key string, value int64, ttl ...time.Duration) (int64, error)
	Del(ctx context.Context, key string) error
	Close(ctx context.Context) error
}

type Config struct {
	Driver string
	Path   string
}

func New(cfg *Config) (Database, error) {
	return NewWithOptions(cfg, 5, 1*time.Second, false)
}

func NewWithOptions(cfg *Config, maxRetries int, retryDelay time.Duration, silent bool) (Database, error) {
	var (
		db  Database
		err error
	)
	switch cfg.Driver {
	case "", "badger":
		db, err = badger.NewWithOptions(cfg.Path, maxRetries, retryDelay, silent)
	default:
		return nil, fmt.Errorf("unsupported database driver: %s", cfg.Driver)
	}
	if err != nil {
		return nil, err
	}
	return db, nil
}
