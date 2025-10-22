package asyncbatch

import (
	"errors"
	"fmt"
	"runtime/debug"
	"sync"
	"sync/atomic"
	"time"
)

var (
	ErrTaskInvalid = errors.New("asyncbatch: task is nil")
	ErrClosed      = errors.New("asyncbatch: group is closed")
)

type options struct {
	Size     int
	Interval time.Duration
	Recover  func(p any, stack []byte)
}

type Option func(*options)

func Size(size int) Option {
	return func(o *options) {
		o.Size = size
	}
}

func Interval(interval time.Duration) Option {
	return func(o *options) {
		o.Interval = interval
	}
}

func Recover(f func(p any, stack []byte)) Option {
	return func(o *options) {
		o.Recover = f
	}
}

func (o *options) Apply(opts ...Option) *options {
	for _, opt := range opts {
		opt(o)
	}
	return o
}

func (o *options) Correct() *options {
	if o.Size <= 0 {
		o.Size = 64
	}
	if o.Interval <= 0 {
		o.Interval = 128 * time.Millisecond
	}
	if o.Recover == nil {
		// 默认的错误处理函数，打印panic信息和堆栈
		o.Recover = func(p any, stack []byte) {
			fmt.Printf("asyncbatch: panic trigger, %v, stack: %s", p, stack)
		}
	}
	return o
}

type Group[Obj any] struct {
	options  *options
	mu       sync.Mutex
	buf      []Obj
	submitCh chan struct{}
	closed   atomic.Bool
	closedCh chan struct{}
	wg       sync.WaitGroup
	task     func(objs []Obj)
}

func New[Obj any](task func(objs []Obj), opts ...Option) (*Group[Obj], error) {
	if task == nil {
		return nil, ErrTaskInvalid
	}
	opt := new(options).Apply(opts...).Correct()
	g := &Group[Obj]{
		mu:       sync.Mutex{},
		buf:      make([]Obj, 0, opt.Size),
		submitCh: make(chan struct{}, 1),
		closed:   atomic.Bool{},
		closedCh: make(chan struct{}),
		wg:       sync.WaitGroup{},
		options:  opt,
		task: func(objs []Obj) {
			defer func() {
				if p := recover(); p != nil {
					opt.Recover(p, debug.Stack())
				}
			}()
			task(objs)
		},
	}
	g.wg.Add(1)
	go g.loop()
	return g, nil
}

func (g *Group[Obj]) Submit(obj Obj) error {
	if g.closed.Load() {
		return ErrClosed
	}
	g.mu.Lock()
	if g.closed.Load() {
		g.mu.Unlock()
		return ErrClosed
	}
	g.buf = append(g.buf, obj)
	if len(g.buf) >= g.options.Size {
		g.mu.Unlock()
		select {
		case g.submitCh <- struct{}{}:
		default:
		}
	} else {
		g.mu.Unlock()
	}
	return nil
}

func (g *Group[Obj]) Close() error {
	if g.closed.Load() {
		return ErrClosed
	}
	g.mu.Lock()
	if g.closed.Load() {
		g.mu.Unlock()
		return ErrClosed
	}
	g.closed.Store(true)
	g.mu.Unlock()
	close(g.closedCh)
	g.wg.Wait()
	return nil
}

func (g *Group[Obj]) loop() {
	defer g.wg.Done()
	ticker := time.NewTicker(g.options.Interval)
	defer ticker.Stop()
	for {
		select {
		case <-g.submitCh:
			g.onSubmit()
		case <-ticker.C:
			g.onTick()
		case <-g.closedCh:
			g.onClose()
			return
		}
	}
}

func (g *Group[Obj]) onSubmit() {
	g.mu.Lock()
	if len(g.buf) < g.options.Size {
		g.mu.Unlock()
		return
	}
	batch := g.buf[0:g.options.Size]
	buf := make([]Obj, len(g.buf)-g.options.Size)
	copy(buf, g.buf[g.options.Size:])
	g.buf = buf
	g.mu.Unlock()
	g.task(batch)
}

func (g *Group[Obj]) onTick() {
	g.mu.Lock()
	if len(g.buf) <= 0 {
		g.mu.Unlock()
		return
	}
	var batch []Obj
	if len(g.buf) >= g.options.Size {
		batch = g.buf[0:g.options.Size]
		buf := make([]Obj, len(g.buf)-g.options.Size)
		copy(buf, g.buf[g.options.Size:])
		g.buf = buf
	} else {
		batch = g.buf
		g.buf = make([]Obj, 0, g.options.Size)
	}

	g.mu.Unlock()
	g.task(batch)
}

func (g *Group[Obj]) onClose() {
	g.mu.Lock()
	batches := chunk(g.buf, g.options.Size)
	g.buf = nil
	g.mu.Unlock()
	for _, batch := range batches {
		g.task(batch)
	}
}

func chunk[S ~[]E, E any](s S, size int) []S {
	l := len(s)
	ss2 := make([]S, 0, (l+size)/size)
	for i := 0; i < l; i += size {
		if i+size < l {
			ss2 = append(ss2, s[i:i+size])
		} else {
			ss2 = append(ss2, s[i:l])
		}
	}
	return ss2
}
