package cache

import (
	"proxy-go/internal/constants"
	"time"
)

func NewCache(ttl time.Duration) *Cache {
	c := &Cache{
		items:   make(map[string]*cacheItem),
		ttl:     ttl,
		maxSize: constants.MaxCacheSize,
	}
	go c.cleanup()
	return c
}

func (c *Cache) Set(key string, value interface{}) {
	c.data.Lock()
	if len(c.items) >= c.maxSize {
		oldest := time.Now()
		var oldestKey string
		for k, v := range c.items {
			if v.timestamp.Before(oldest) {
				oldest = v.timestamp
				oldestKey = k
			}
		}
		delete(c.items, oldestKey)
	}
	c.items[key] = &cacheItem{
		value:     value,
		timestamp: time.Now(),
	}
	c.data.Unlock()
}

func (c *Cache) Get(key string) (interface{}, bool) {
	c.data.RLock()
	item, exists := c.items[key]
	c.data.RUnlock()

	if !exists {
		return nil, false
	}

	if time.Since(item.timestamp) > c.ttl {
		c.data.Lock()
		delete(c.items, key)
		c.data.Unlock()
		return nil, false
	}

	return item.value, true
}

func (c *Cache) cleanup() {
	ticker := time.NewTicker(c.ttl)
	for range ticker.C {
		now := time.Now()
		c.data.Lock()
		for key, item := range c.items {
			if now.Sub(item.timestamp) > c.ttl {
				delete(c.items, key)
			}
		}
		c.data.Unlock()
	}
}
