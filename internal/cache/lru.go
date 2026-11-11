package cache

import "sync"

// 内存 LRU 索引 (缓存键 → 缓存项)

// LRU 缓存节点
type LRUNode struct {
	key   CacheKey
	value *CacheItem
	prev  *LRUNode
	next  *LRUNode
}

// LRU 缓存实现
type LRUCache struct {
	capacity int
	size     int
	head     *LRUNode
	tail     *LRUNode
	cache    map[CacheKey]*LRUNode
	mu       sync.RWMutex
}

// NewLRUCache 创建LRU缓存
func NewLRUCache(capacity int) *LRUCache {
	lru := &LRUCache{
		capacity: capacity,
		cache:    make(map[CacheKey]*LRUNode),
		head:     &LRUNode{},
		tail:     &LRUNode{},
	}
	lru.head.next = lru.tail
	lru.tail.prev = lru.head
	return lru
}

// Get 从LRU缓存中获取
func (lru *LRUCache) Get(key CacheKey) (*CacheItem, bool) {
	lru.mu.Lock()
	defer lru.mu.Unlock()

	if node, exists := lru.cache[key]; exists {
		lru.moveToHead(node)
		return node.value, true
	}
	return nil, false
}

// Put 向LRU缓存中添加
func (lru *LRUCache) Put(key CacheKey, value *CacheItem) {
	lru.mu.Lock()
	defer lru.mu.Unlock()

	if node, exists := lru.cache[key]; exists {
		node.value = value
		lru.moveToHead(node)
	} else {
		newNode := &LRUNode{key: key, value: value}
		lru.cache[key] = newNode
		lru.addToHead(newNode)
		lru.size++

		if lru.size > lru.capacity {
			tail := lru.removeTail()
			delete(lru.cache, tail.key)
			lru.size--
		}
	}
}

// Delete 从LRU缓存中删除
func (lru *LRUCache) Delete(key CacheKey) {
	lru.mu.Lock()
	defer lru.mu.Unlock()

	if node, exists := lru.cache[key]; exists {
		lru.removeNode(node)
		delete(lru.cache, key)
		lru.size--
	}
}

// moveToHead 将节点移到头部
func (lru *LRUCache) moveToHead(node *LRUNode) {
	lru.removeNode(node)
	lru.addToHead(node)
}

// addToHead 添加到头部
func (lru *LRUCache) addToHead(node *LRUNode) {
	node.prev = lru.head
	node.next = lru.head.next
	lru.head.next.prev = node
	lru.head.next = node
}

// removeNode 移除节点
func (lru *LRUCache) removeNode(node *LRUNode) {
	node.prev.next = node.next
	node.next.prev = node.prev
}

// removeTail 移除尾部节点
func (lru *LRUCache) removeTail() *LRUNode {
	lastNode := lru.tail.prev
	lru.removeNode(lastNode)
	return lastNode
}

// Range 遍历所有缓存项
func (lru *LRUCache) Range(fn func(key CacheKey, value *CacheItem) bool) {
	lru.mu.RLock()
	defer lru.mu.RUnlock()

	for key, node := range lru.cache {
		if !fn(key, node.value) {
			break
		}
	}
}

// Size 返回缓存大小
func (lru *LRUCache) Size() int {
	lru.mu.RLock()
	defer lru.mu.RUnlock()
	return lru.size
}
