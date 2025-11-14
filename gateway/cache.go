package main

import (
	"container/list"
	"sync"
	"time"
)

// Cache 缓存接口
type Cache interface {
	Get(key string) ([]byte, bool)
	Set(key string, value []byte)
	Delete(key string)
	Clear()
	Size() int
}

// LRUCache 带 TTL 的 LRU 缓存
type LRUCache struct {
	maxSize         int
	ttl             time.Duration
	items           map[string]*list.Element
	evictList       *list.List
	mu              sync.RWMutex
	stopCleanup     chan struct{}
}

// cacheEntry 缓存条目
type cacheEntry struct {
	key        string
	value      []byte
	expireTime time.Time
}

// NewCache 创建缓存
func NewCache(config CacheConfig) *LRUCache {
	if !config.Enabled {
		return nil
	}

	cache := &LRUCache{
		maxSize:     config.MaxSize,
		ttl:         config.TTL,
		items:       make(map[string]*list.Element),
		evictList:   list.New(),
		stopCleanup: make(chan struct{}),
	}

	// 启动清理协程
	go cache.cleanupRoutine(config.CleanupInterval)

	return cache
}

// Get 获取缓存
func (c *LRUCache) Get(key string) ([]byte, bool) {
	if c == nil {
		return nil, false
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	element, exists := c.items[key]
	if !exists {
		return nil, false
	}

	entry := element.Value.(*cacheEntry)

	// 检查是否过期
	if time.Now().After(entry.expireTime) {
		c.removeElement(element)
		return nil, false
	}

	// 移动到最前面（最近使用）
	c.evictList.MoveToFront(element)

	return entry.value, true
}

// Set 设置缓存
func (c *LRUCache) Set(key string, value []byte) {
	if c == nil {
		return
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	// 如果已存在，更新值
	if element, exists := c.items[key]; exists {
		c.evictList.MoveToFront(element)
		entry := element.Value.(*cacheEntry)
		entry.value = value
		entry.expireTime = time.Now().Add(c.ttl)
		return
	}

	// 创建新条目
	entry := &cacheEntry{
		key:        key,
		value:      value,
		expireTime: time.Now().Add(c.ttl),
	}

	element := c.evictList.PushFront(entry)
	c.items[key] = element

	// 检查是否超过最大大小
	if c.evictList.Len() > c.maxSize {
		c.evictOldest()
	}
}

// Delete 删除缓存
func (c *LRUCache) Delete(key string) {
	if c == nil {
		return
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	if element, exists := c.items[key]; exists {
		c.removeElement(element)
	}
}

// Clear 清空缓存
func (c *LRUCache) Clear() {
	if c == nil {
		return
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	c.items = make(map[string]*list.Element)
	c.evictList.Init()
}

// Size 返回缓存大小
func (c *LRUCache) Size() int {
	if c == nil {
		return 0
	}

	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.evictList.Len()
}

// evictOldest 淘汰最旧的条目
func (c *LRUCache) evictOldest() {
	element := c.evictList.Back()
	if element != nil {
		c.removeElement(element)
	}
}

// removeElement 移除元素
func (c *LRUCache) removeElement(element *list.Element) {
	c.evictList.Remove(element)
	entry := element.Value.(*cacheEntry)
	delete(c.items, entry.key)
}

// Cleanup 清理过期条目
func (c *LRUCache) Cleanup() {
	if c == nil {
		return
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	now := time.Now()
	var toRemove []*list.Element

	// 找出所有过期的条目
	for element := c.evictList.Back(); element != nil; element = element.Prev() {
		entry := element.Value.(*cacheEntry)
		if now.After(entry.expireTime) {
			toRemove = append(toRemove, element)
		}
	}

	// 移除过期条目
	for _, element := range toRemove {
		c.removeElement(element)
	}
}

// cleanupRoutine 定期清理协程
func (c *LRUCache) cleanupRoutine(interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			c.Cleanup()
		case <-c.stopCleanup:
			return
		}
	}
}

// Stop 停止缓存
func (c *LRUCache) Stop() {
	if c != nil {
		close(c.stopCleanup)
	}
}
