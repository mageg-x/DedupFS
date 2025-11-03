package utils

import (
	"sync"
	"time"
)

// Delayer 是防抖器接口
type Delayer interface {
	Call(fn func())
	Stop()
}

// debouncer 是内部实现
type delayer struct {
	delay time.Duration
	mu    sync.Mutex
	timer *time.Timer
}

// New 创建一个新的防抖器
func NewDelayer(delay time.Duration) Delayer {
	return &delayer{delay: delay}
}

// Call 触发防抖：取消之前的定时器，设置新的
func (d *delayer) Call(fn func()) {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.timer != nil {
		d.timer.Stop() // 取消未触发的定时器
	}

	d.timer = time.AfterFunc(d.delay, fn)
}
func (d *delayer) Stop() {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.timer != nil {
		d.timer.Stop() // 取消未触发的定时器
	}
}
