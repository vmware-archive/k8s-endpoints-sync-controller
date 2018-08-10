// Copyright © 2018 VMware, Inc. All Rights Reserved.
// SPDX-License-Identifier: MIT

package utils

import (
	"sync"
)

type ConcurrentMap struct {
	sync.RWMutex
	internal map[string]bool
}

func NewConcurrentMap() *ConcurrentMap {
	return &ConcurrentMap{
		internal: make(map[string]bool),
	}
}

func (rm *ConcurrentMap) Load(key string) (value bool) {
	rm.RLock()
	result, _ := rm.internal[key]
	rm.RUnlock()
	return result
}

func (rm *ConcurrentMap) Delete(key string) {
	rm.Lock()
	delete(rm.internal, key)
	rm.Unlock()
}

func (rm *ConcurrentMap) Store(key string, value bool) {
	rm.Lock()
	rm.internal[key] = value
	rm.Unlock()
}
