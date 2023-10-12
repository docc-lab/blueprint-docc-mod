package clientpool

import (
	"context"
	"sync"
	"sync/atomic"
)

type ClientPool[T any] struct {
	lock         sync.Mutex
	wait_channel chan T
	fn           func() (T, error)
	maxClients   int64
	curClients   int64
	waiting      int64
}

func NewClientPool[T any](maxClients int64, fn func() (T, error)) *ClientPool[T] {
	wait_channel := make(chan T, maxClients)
	return &ClientPool[T]{wait_channel: wait_channel, fn: fn, maxClients: maxClients, curClients: 0, waiting: 0}
}

// /* The below is from blueprint v1; TODO to implement metrics in blueprint v2 */
// func (this *ClientPool[T]) StartMetricsThread(pool_id string) {
// 	go func() {
// 		ticker := time.NewTicker(1 * time.Second)
// 		for {
// 			select {
// 			case <-ticker.C:
// 				debug.ReportMetric(pool_id+":FreeClients", len(this.wait_channel))
// 				debug.ReportMetric(pool_id+":CurrentWaiting", this.waiting)
// 			}
// 		}
// 	}()
// }

func (this *ClientPool[T]) Pop(ctx context.Context) (client T, err error) {
	this.lock.Lock()
	if this.curClients < this.maxClients {
		defer this.lock.Unlock()
		if client, err = this.fn(); err == nil {
			this.curClients += 1
		}
		return
	}
	this.lock.Unlock()
	atomic.AddInt64(&this.waiting, 1)
	select {
	case <-ctx.Done():
		return
	case client = <-this.wait_channel:
		atomic.AddInt64(&this.waiting, -1)
		return
	}
}

func (this *ClientPool[T]) Push(client T) {
	this.wait_channel <- client
}