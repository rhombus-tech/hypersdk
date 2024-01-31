package main

import (
	"fmt"
	"sync"
	"sync/atomic"
)

type MonotonicCounter struct {
	value int64
}

func (c *MonotonicCounter) Increment() int64 {
	return atomic.AddInt64(&c.value, 1)
}

func (c *MonotonicCounter) GetValue() int64 {
	return atomic.LoadInt64(&c.value)
}

func main() {
	counter := MonotonicCounter{}

	var wg sync.WaitGroup
	wg.Add(3)

	go func() {
		defer wg.Done()
		for i := 0; i < 10; i++ {
			fmt.Println("Goroutine 1 - Counter Value:", counter.Increment())
		}
	}()

	go func() {
		defer wg.Done()
		for i := 0; i < 10; i++ {
			fmt.Println("Goroutine 2 - Counter Value:", counter.Increment())
		}
	}()

	go func() {
		defer wg.Done()
		for i := 0; i < 10; i++ {
			fmt.Println("Goroutine 3 - Counter Value:", counter.Increment())
		}
	}()

	wg.Wait()

	fmt.Println("Final Counter Value:", counter.GetValue())
}
