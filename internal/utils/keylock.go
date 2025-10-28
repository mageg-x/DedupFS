/*
 * Copyright (C) 2025-2025 raochaoxun <raochaoxun@gmail.com>.
 *
 * This program is free software: you can redistribute it and/or modify
 * it under the terms of the GNU General Public License as published by
 * the Free Software Foundation, either version 3 of the License, or
 * (at your option) any later version.
 *
 * This program is distributed in the hope that it will be useful,
 * but WITHOUT ANY WARRANTY; without even the implied warranty of
 * MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
 * GNU General Public License for more details.
 *
 * You should have received a copy of the GNU General Public License
 * along with this program.  If not, see <https://www.gnu.org/licenses/>.
 */
package utils

import (
	"sync"

	"github.com/cespare/xxhash/v2"
)

const shardCount = 1024

var (
	_keylocks [shardCount]sync.Mutex
)

func For(key string) sync.Locker {
	sum := xxhash.Sum64([]byte(key))
	return &_keylocks[sum%shardCount]
}

func Lock(key string) {
	lock := For(key)
	lock.Lock()
}

func Unlock(key string) {
	lock := For(key)
	lock.Unlock()
}

func WithLockKey(key string, fn func() error) error {
	l := For(key)
	l.Lock()
	defer l.Unlock()
	return fn()
}
