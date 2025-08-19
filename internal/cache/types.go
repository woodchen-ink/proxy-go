package cache

import (
	"sync"
	"time"
)

type Cache struct {
	data    sync.RWMutex
	items   map[string]*cacheItem
	ttl     time.Duration
	maxSize int
}

type cacheItem struct {
	value     interface{}
	timestamp time.Time
}
