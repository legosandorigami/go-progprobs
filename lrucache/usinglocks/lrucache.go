package usinglocks

import (
	"fmt"
	"sync"
	"time"
)

const MAX_CAPACITY = 1024

type item struct {
	key        string
	value      []byte
	expiration int64
	ttl        time.Duration
}

type linkedList struct {
	val        item
	prev, next *linkedList
}

type lruCacheImpl struct {
	maxCapacity    uint64
	head           *linkedList
	tail           *linkedList
	store          map[string]*linkedList
	mu             *sync.RWMutex
	stopWorkerChan chan bool
	wg             *sync.WaitGroup
}

func (lruc *lruCacheImpl) Get(key string) ([]byte, bool) {
	lruc.mu.Lock()
	defer lruc.mu.Unlock()
	ll, ok := lruc.store[key]

	if ok {
		now := time.Now().UnixNano()
		// updating the expiration
		ll.val.expiration = now + ll.val.ttl.Nanoseconds()
		lruc.removeNode(ll)
		lruc.moveToFront(ll)

		return ll.val.value, ok
	}

	return make([]byte, 0), ok
}

func (lruc *lruCacheImpl) Set(key string, value []byte, ttl time.Duration) (bool, bool) {
	lruc.mu.Lock()
	defer lruc.mu.Unlock()
	ll, ok := lruc.store[key]
	if ok {
		ll.val.value = value
		now := time.Now().UnixNano()
		// updating the expiration
		ll.val.ttl = ttl
		ll.val.expiration = now + ttl.Nanoseconds()
		lruc.removeNode(ll)
		lruc.moveToFront(ll)

		return ok, false
	}
	isEvicted := false
	now := time.Now().UnixNano()
	ll = &linkedList{
		val: item{
			key:        key,
			value:      value,
			ttl:        ttl,
			expiration: now + ttl.Nanoseconds(),
		},
	}
	if len(lruc.store) < int(lruc.maxCapacity) {
		lruc.store[key] = ll
		if len(lruc.store) == 1 {
			lruc.head, lruc.tail = ll, ll
		} else {
			lruc.moveToFront(ll)
		}

	} else {
		isEvicted = true
		lruc.store[key] = ll

		lruc.moveToFront(ll)

		keyToRemove := lruc.tail.val.key
		delete(lruc.store, keyToRemove)

		lruc.updateTail()
	}

	return ok, isEvicted
}

func (lruc *lruCacheImpl) removeNode(ll *linkedList) {
	// moving the item to the fron of the linked list
	prev, next := ll.prev, ll.next

	// checking if "ll" is actually the tail of the linked list
	if lruc.head != lruc.tail && ll == lruc.tail {
		lruc.tail = prev
	}

	if prev != nil {
		prev.next = next
	}
	if next != nil {
		next.prev = prev
	}

	ll.prev = nil
}

func (lruc *lruCacheImpl) moveToFront(ll *linkedList) {
	if ll.val.key == lruc.head.val.key {
		return
	}
	lruc.head.prev = ll
	ll.next = lruc.head
	lruc.head = ll
}

// called only on eviction
func (lruc *lruCacheImpl) updateTail() {
	prev := lruc.tail.prev
	if prev != nil {
		prev.next = nil
		lruc.tail = prev
	}
}

func (lruc *lruCacheImpl) Stop(indicator chan bool) {
	close(lruc.stopWorkerChan)
	lruc.wg.Wait()
	close(indicator)
}

func (lruc *lruCacheImpl) Delete(toDeleteKeys []string) {
	lruc.mu.Lock()
	defer lruc.mu.Unlock()
	for _, key := range toDeleteKeys {
		if ll, ok := lruc.store[key]; ok {
			delete(lruc.store, key)
			prev, next := ll.prev, ll.next
			if prev != nil {
				prev.next = next
			}
			if next != nil {
				next.prev = prev
			}
			ll.prev, ll.next = nil, nil
		}
	}
}

func (lruc *lruCacheImpl) startWorker(interval time.Duration) {
	ticker := time.NewTicker(interval)
	for {
		select {
		case <-ticker.C:
			toDeleteKeys := make([]string, 0)
			now := time.Now().UnixNano()
			lruc.mu.RLock()
			for k, v := range lruc.store {
				if now > v.val.expiration {
					toDeleteKeys = append(toDeleteKeys, k)
				}
			}
			lruc.mu.RUnlock()
			lruc.Delete(toDeleteKeys)
		case <-lruc.stopWorkerChan:
			ticker.Stop()
			lruc.wg.Done()
			return
		}
	}
}

func NewLRUCache(capacity uint64) (*lruCacheImpl, error) {
	if capacity == 0 {
		return nil, fmt.Errorf("capacity must be positive integer. given: %d", capacity)
	}
	lruc := &lruCacheImpl{
		maxCapacity:    capacity,
		store:          make(map[string]*linkedList),
		mu:             &sync.RWMutex{},
		wg:             &sync.WaitGroup{},
		stopWorkerChan: make(chan bool),
	}
	lruc.wg.Add(1)

	go lruc.startWorker(1 * time.Second)

	return lruc, nil
}
