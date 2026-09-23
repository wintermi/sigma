// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

// Package contextlock provides cancelable ownership for serialized operations.
package contextlock

import (
	"context"
	"sync"
)

// Mutex is a context-aware mutex. Its zero value is ready for use.
// A Mutex must not be copied after first use.
type Mutex struct {
	once  sync.Once
	owned chan struct{}
}

// Lock acquires ownership or returns the context error without ownership.
func (m *Mutex) Lock(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	m.once.Do(func() { m.owned = make(chan struct{}, 1) })
	select {
	case <-ctx.Done():
		return ctx.Err()
	case m.owned <- struct{}{}:
		if err := ctx.Err(); err != nil {
			m.Unlock()
			return err
		}
		return nil
	}
}

// Unlock releases ownership obtained by a successful Lock.
func (m *Mutex) Unlock() { <-m.owned }
