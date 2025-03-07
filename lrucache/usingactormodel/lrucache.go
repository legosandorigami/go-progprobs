package usingactormodel

import (
	"fmt"
	"sync"
	"time"
)

type MSG_TYPE int

const MSG_CHANNEL_SIZE = 1024

const MAX_CAPACITY = 1024

const (
	SET_MSG MSG_TYPE = iota
	GET_MSG
	DELETE_MSG
)

type item struct {
	key   string
	value []byte
	// expiration int64
	ttl time.Duration
}

type linkedList struct {
	val        item
	prev, next *linkedList
}

type response struct {
	val              []byte
	existed, evicted bool
}

type msg struct {
	msg      MSG_TYPE
	key      string
	val      []byte
	ttl      time.Duration
	respChan chan response
}

type lruCacheImpl struct {
	maxCapacity   uint64
	head          *linkedList
	tail          *linkedList
	store         map[string]*linkedList
	keyTimerMap   map[string]*time.Timer
	workerMsgChan chan msg
	wg            *sync.WaitGroup
}

func (lruc *lruCacheImpl) Get(key string) ([]byte, bool) {
	ch := make(chan response)

	lruc.workerMsgChan <- msg{
		msg:      GET_MSG,
		key:      key,
		respChan: ch,
	}

	result := <-ch

	return result.val, result.existed
}

func (lruc *lruCacheImpl) get(key string) ([]byte, bool) {
	ll, ok := lruc.store[key]

	if ok {
		// updating the expiration
		lruc.setExpiry(key, ll.val.ttl)

		lruc.removeNode(ll)
		lruc.moveToFront(ll)

		return ll.val.value, ok
	}

	return make([]byte, 0), ok
}

func (lruc *lruCacheImpl) Set(key string, value []byte, ttl time.Duration) (bool, bool) {
	ch := make(chan response)

	lruc.workerMsgChan <- msg{
		msg:      SET_MSG,
		key:      key,
		val:      value,
		ttl:      ttl,
		respChan: ch,
	}

	result := <-ch

	return result.existed, result.evicted
}

func (lruc *lruCacheImpl) set(key string, value []byte, ttl time.Duration) (bool, bool) {
	ll, ok := lruc.store[key]
	if ok {
		ll.val.value = value

		// updating the expiration
		lruc.setExpiry(key, ttl)

		lruc.removeNode(ll)
		lruc.moveToFront(ll)

		return ok, false
	}
	isEvicted := false
	ll = &linkedList{
		val: item{
			key:   key,
			value: value,
			ttl:   ttl,
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

func (lruc *lruCacheImpl) setExpiry(key string, ttl time.Duration) {
	stopTimer, ok := lruc.keyTimerMap[key]
	if ok {
		stopTimer.Stop()
	}
	lruc.keyTimerMap[key] = time.AfterFunc(ttl, func() {
		lruc.workerMsgChan <- msg{
			msg: DELETE_MSG,
			key: key,
		}
	})
}

func (lruc *lruCacheImpl) Stop(indicator chan bool) {
	close(lruc.workerMsgChan)
	lruc.wg.Wait()

	for _, t := range lruc.keyTimerMap {
		t.Stop()
	}

	close(indicator)
}

func (lruc *lruCacheImpl) delete(key string) {
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

	// making sure to stop the timer and delete from the expiry map
	if t, ok := lruc.keyTimerMap[key]; ok {
		t.Stop()
	}
	delete(lruc.keyTimerMap, key)
}

func (lruc *lruCacheImpl) startWorker() {
	for msg_ := range lruc.workerMsgChan {
		switch msg_.msg {
		case GET_MSG:
			fmt.Println("Get called")
			val, exists := lruc.get(msg_.key)
			msg_.respChan <- response{
				val:     val,
				existed: exists,
			}
		case SET_MSG:
			fmt.Println("Set called")
			existed, evicted := lruc.set(msg_.key, msg_.val, msg_.ttl)
			msg_.respChan <- response{
				existed: existed,
				evicted: evicted,
			}
		case DELETE_MSG:
			fmt.Println("Delete called")
			lruc.delete(msg_.key)
		}
	}
	lruc.wg.Done()
}

func NewLRUCache(capacity uint64) (*lruCacheImpl, error) {
	if capacity == 0 {
		return nil, fmt.Errorf("capacity must be positive integer. given: %d", capacity)
	}
	lruc := &lruCacheImpl{
		maxCapacity:   capacity,
		store:         make(map[string]*linkedList),
		keyTimerMap:   make(map[string]*time.Timer),
		wg:            &sync.WaitGroup{},
		workerMsgChan: make(chan msg, MSG_CHANNEL_SIZE),
	}
	lruc.wg.Add(1)

	go lruc.startWorker()

	return lruc, nil
}
