package main

type HashSet struct {
	capacity int
	cache    map[string][]byte
}

func NewHashSet(capacity int) *HashSet {
	return &HashSet{
		capacity: capacity,
		cache:    make(map[string][]byte),
	}
}

func (c *HashSet) Get(key string) (bool, []byte) {
	if value, ok := c.cache[key]; ok {
		return true, value
	}
	return false, nil
}

func (c *HashSet) Put(key string) {
	c.cache[key] = []byte(key)
}

func (c *HashSet) PutBytes(value []byte) {
	key := string(value)
	c.cache[key] = []byte(key)
}

func (c *HashSet) PutBoth(key string, value []byte) {
	c.cache[key] = value
}

func (c *HashSet) SurfaceMap() map[string][]byte {
	return c.cache
}

func (c *HashSet) Clear() {
	for k := range c.cache {
		delete(c.cache, k)
	}
}
