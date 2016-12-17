package cache

import (
	"sync"
	"time"

	mgo "gopkg.in/mgo.v2"
)

// MongoCache holds the cache values that will be stored in mongoDB
type MongoCache struct {
	mongeSession *mgo.Session
	// cache holds the cache data

	CollectionName string
	// ttl is a duration for a cache key to expire
	TTL time.Duration

	GCInterval time.Duration

	// StartGC starts the garbage collector and deletes the
	// expired keys from mongo with given time interval
	StartGC bool

	// gcTicker controls gc intervals
	gcTicker *time.Ticker

	// done controls sweeping goroutine lifetime
	done chan struct{}

	// Mutex is used for handling the concurrent
	// read/write requests for cache
	sync.RWMutex
}

// NewMongoCacheWithTTL creates a caching layer backed by mongo. TTL's are
// managed either by a background cleaner or document is removed on the Get
// operation. Mongo TTL indexes are not utilized since there can be multiple
// systems using the same collection with different TTL values.
//
// The responsibility of stopping the GC process belongs to the user.
//
// Session is not closed while stopping the GC.
// This function satisfy you to not pass nil value to the function as parameter
// e.g (usage) :
// configure with defaults, just call;
// NewMongoCacheWithTTL(session)
//
// configure ttl duration with;
// NewMongoCacheWithTTL(session, func(m *MongoCache) {
// m.TTL = 2 * time.Minute
// })
//
// configure collection name with;
// NewMongoCacheWithTTL(session, func(m *MongoCache) {
// m.CollectionName = "MongoCacheCollectionName"
// })
func NewMongoCacheWithTTL(session *mgo.Session, configs ...func(*MongoCache)) Cache {
	mc := &MongoCache{
		mongeSession:   session,
		TTL:            defaultExpireDuration,
		CollectionName: defaultKeyValueColl,
		GCInterval:     time.Minute,
		StartGC:        false,
	}

	for _, configFunc := range configs {
		configFunc(mc)
	}

	if mc.StartGC {
		mc.StartGCol(mc.GCInterval)
	}

	return mc
}

// WithStartGC adds the given value to the WithStartGC
// this is an external way to change WithStartGC value as true
// recommended way is : add together with NewMongoCacheWithTTL()
func (m *MongoCache) WithStartGC(isStart bool) *MongoCache {
	m.StartGC = isStart
	return m
}

// Get returns a value of a given key if it exists
func (m *MongoCache) Get(key string) (interface{}, error) {
	return m.GetKeyWithExpireCheck(key)
}

// Set will persist a value to the cache or
// override existing one with the new one
func (m *MongoCache) Set(key string, value interface{}) error {
	return m.set(key, value)
}

// Delete deletes a given key if exists
func (m *MongoCache) Delete(key string) error {
	return m.DeleteKey(key)
}

func (m *MongoCache) set(key string, value interface{}) error {
	kv := &KeyValue{
		Key:       key,
		Value:     value,
		CreatedAt: time.Now().UTC(),
		ExpireAt:  time.Now().UTC().Add(m.TTL),
	}

	return m.CreateKeyValueWithExpiration(kv)
}

// StartGCol starts the garbage collector with given time interval
func (m *MongoCache) StartGCol(gcInterval time.Duration) {
	if gcInterval <= 0 {
		return
	}

	ticker := time.NewTicker(gcInterval)
	done := make(chan struct{})

	m.Lock()
	m.gcTicker = ticker
	m.done = done
	m.Unlock()

	go func() {
		for {
			select {
			case <-ticker.C:
				m.Lock()
				m.DeleteExpiredKeys()
				m.Unlock()
			case <-done:
				return
			}
		}
	}()
}

// StopGC stops sweeping goroutine.
func (r *MemoryTTL) StopGCol() {
	if r.gcTicker != nil {
		r.Lock()
		r.gcTicker.Stop()
		r.gcTicker = nil
		close(r.done)
		r.done = nil
		r.Unlock()
	}
}
