package utils

import (
	"container/list"
	"sync"
)

// LRUCache is a thread-safe LRU cache for deduplication.
type LRUCache struct {
	capacity int
	mu       sync.Mutex
	items    map[string]*list.Element
	order    *list.List // front = newest, back = oldest
}

// NewLRUCache creates an LRU cache with the given capacity.
func NewLRUCache(capacity int) *LRUCache {
	if capacity <= 0 {
		capacity = 1000
	}
	return &LRUCache{
		capacity: capacity,
		items:    make(map[string]*list.Element, capacity),
		order:    list.New(),
	}
}

// Contains checks if the key exists and promotes it to the front.
func (c *LRUCache) Contains(key string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if elem, ok := c.items[key]; ok {
		c.order.MoveToFront(elem)
		return true
	}
	return false
}

// Add inserts a key. If already present, promotes it. If over capacity, evicts the oldest.
func (c *LRUCache) Add(key string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if elem, ok := c.items[key]; ok {
		c.order.MoveToFront(elem)
		return
	}
	elem := c.order.PushFront(key)
	c.items[key] = elem
	for c.order.Len() > c.capacity {
		oldest := c.order.Back()
		if oldest != nil {
			c.order.Remove(oldest)
			delete(c.items, oldest.Value.(string))
		}
	}
}

// Len returns the number of items in the cache.
func (c *LRUCache) Len() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.order.Len()
}
