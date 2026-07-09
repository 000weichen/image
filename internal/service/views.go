package service

import (
	"context"
	"log"
	"sync"
	"time"
)

const viewFlushInterval = 3 * time.Second

// viewCounter 在内存中聚合图片访问计数，由后台协程定期批量写回数据库。
// 公开图片访问是最高频路径，逐次 UPDATE 会让 SQLite 的单写者成为瓶颈，
// 也会让恶意刷图片请求直接放大成数据库写压力；代价是计数落库最多延迟一个刷新周期。
type viewCounter struct {
	repo *Repository

	mu     sync.Mutex
	counts map[int64]int64

	stopOnce sync.Once
	stop     chan struct{}
	done     chan struct{}
}

func newViewCounter(repo *Repository) *viewCounter {
	vc := &viewCounter{
		repo:   repo,
		counts: make(map[int64]int64),
		stop:   make(chan struct{}),
		done:   make(chan struct{}),
	}
	go vc.loop()
	return vc
}

func (vc *viewCounter) add(id int64) {
	vc.mu.Lock()
	vc.counts[id]++
	vc.mu.Unlock()
}

func (vc *viewCounter) loop() {
	defer close(vc.done)
	ticker := time.NewTicker(viewFlushInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			vc.flush()
		case <-vc.stop:
			vc.flush()
			return
		}
	}
}

func (vc *viewCounter) flush() {
	vc.mu.Lock()
	if len(vc.counts) == 0 {
		vc.mu.Unlock()
		return
	}
	pending := vc.counts
	vc.counts = make(map[int64]int64)
	vc.mu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := vc.repo.addViewsBatch(ctx, pending); err != nil {
		log.Printf("写回访问计数失败（%d 张图片，下一轮重试）: %v", len(pending), err)
		// 合并回缓冲等待下一轮，避免计数丢失。
		vc.mu.Lock()
		for id, n := range pending {
			vc.counts[id] += n
		}
		vc.mu.Unlock()
	}
}

// Close 停止后台刷新，并把剩余计数写回数据库后返回。
func (vc *viewCounter) Close() {
	vc.stopOnce.Do(func() { close(vc.stop) })
	<-vc.done
}
