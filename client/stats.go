package main

import (
	"log"
	"sync/atomic"
	"time"
)

type Stats struct {
	ActiveConnections int32
	TotalBytesUp      int64
	TotalBytesDown    int64
}

func NewStats() *Stats {
	return &Stats{}
}

func (s *Stats) RunLoop(shutdown <-chan struct{}, interval time.Duration) {
	if interval <= 0 {
		interval = 30 * time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-shutdown:
			return
		case <-ticker.C:
			active := atomic.LoadInt32(&s.ActiveConnections)
			up := atomic.LoadInt64(&s.TotalBytesUp)
			down := atomic.LoadInt64(&s.TotalBytesDown)
			if active == 0 && up == 0 && down == 0 {
				continue
			}
			totalMB := float64(up+down) / (1024.0 * 1024.0)
			log.Printf("[СТАТИСТИКА] Активных: %d | Трафик: %.2f МБ", active, totalMB)
		}
	}
}
