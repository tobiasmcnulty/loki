package main

import "container/list"

type LRUCache4 struct {
	capacity int
	cache    map[string]*list.Element
	list     *list.List
}

type Entry4 struct {
	key   string
	value []byte
}

func NewLRUCache4(capacity int) *LRUCache4 {
	return &LRUCache4{
		capacity: capacity,
		cache:    make(map[string]*list.Element),
		list:     list.New(),
	}
}

func (c *LRUCache4) Get(value []byte) bool {
	key := string(value)
	if elem, ok := c.cache[key]; ok {
		// Move the accessed element to the front of the list
		c.list.MoveToFront(elem)
		return true
	}
	return false
}

func (c *LRUCache4) GetString(key string) (bool, []byte) {
	if elem, ok := c.cache[key]; ok {
		// Move the accessed element to the front of the list
		c.list.MoveToFront(elem)
		return true, elem.Value.(*Entry4).value
	}
	return false, nil
}

func (c *LRUCache4) Put(value []byte) {
	key := string(value)

	if elem, ok := c.cache[key]; ok {
		// If the key already exists, move it to the front
		c.list.MoveToFront(elem)
	} else {
		// If the cache is full, remove the least recently used element
		if len(c.cache) >= c.capacity {
			// Get the least recently used element from the back of the list
			tailElem := c.list.Back()
			if tailElem != nil {
				deletedEntry := c.list.Remove(tailElem).(*Entry4)
				delete(c.cache, deletedEntry.key)
			}
		}

		// Add the new key to the cache and the front of the list
		newEntry := &Entry4{key, value}
		newElem := c.list.PushFront(newEntry)
		c.cache[key] = newElem
	}
}

func (c *LRUCache4) Clear() {
	// Iterate through the list and remove all elements
	for elem := c.list.Front(); elem != nil; elem = elem.Next() {
		delete(c.cache, elem.Value.(*Entry4).key)
	}

	// Clear the list
	c.list.Init()
}
