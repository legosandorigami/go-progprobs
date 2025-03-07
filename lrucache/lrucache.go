package main

import (
	"fmt"
	"time"

	"example.com/go-progprobs-lrucache/usingactormodel"
	// "example.com/go-progprobs-lrucache/usinglocks"
)

type LRUCache interface {
	Get(string) ([]byte, bool)
	Set(string, []byte, time.Duration) (bool, bool)
}

func lruCacheDriver(lrucache LRUCache) {
	for i := 0; i < 32; i++ {
		v := fmt.Sprintf("%d", i)
		_, evicted := lrucache.Set(v, []byte(v), 20*time.Millisecond)
		time.Sleep(20 * time.Millisecond)
		fmt.Printf("inserted value: %s evicted: %v\n", v, evicted)
	}

	for i := 0; i < 32; i++ {
		if i%5 == 0 {
			v := fmt.Sprintf("%d", i)
			_, exist := lrucache.Get(v)
			fmt.Printf("value: %s exists: %v\n", v, exist)
			time.Sleep(20 * time.Millisecond)
		}
	}

	time.Sleep(1 * time.Second)

	fmt.Println("After sleeping")

	for i := 0; i < 32; i++ {
		if i%2 == 0 || i%5 == 0 {
			v := fmt.Sprintf("%d", i)
			_, exist := lrucache.Get(v)
			fmt.Printf("value: %s exists: %v\n", v, exist)
			time.Sleep(20 * time.Millisecond)
		}
	}

	for i := 0; i < 32; i++ {
		if i%5 == 0 {
			v := fmt.Sprintf("%d", i)
			existed, _ := lrucache.Set(v, []byte(v), 10*time.Millisecond)
			fmt.Printf("value: %s existed: %v\n", v, existed)
		}
	}
}

func main() {
	lrucache, _ := usingactormodel.NewLRUCache(16)
	stopSignal := make(chan bool)
	defer func() {
		lrucache.Stop(stopSignal)
		<-stopSignal
	}()
	lruCacheDriver(lrucache)
}
