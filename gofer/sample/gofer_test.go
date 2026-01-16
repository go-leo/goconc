package sample_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/soyacen/goconc/gofer/sample"
)

// TestNew 测试创建新的gofer实例
func TestNew(t *testing.T) {
	// 测试默认配置
	g := sample.New()
	if g == nil {
		t.Error("Expected non-nil gofer instance")
	}

	// 测试自定义配置
	g = sample.New(
		sample.CorePoolSize(5),
		sample.MaximumPoolSize(10),
		sample.KeepAliveTime(time.Second),
	)
	if g == nil {
		t.Error("Expected non-nil gofer instance with custom options")
	}
}

// TestGoWithNilTask 测试提交nil任务
func TestGoWithNilTask(t *testing.T) {
	g := sample.New()
	err := g.Go(nil)
	if err == nil {
		t.Error("Expected error when submitting nil task")
	}
}

// TestGoWithClosedPool 测试向已关闭的池提交任务
func TestGoWithClosedPool(t *testing.T) {
	g := sample.New()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	// 关闭池
	err := g.Close(ctx)
	if err != nil {
		t.Errorf("Unexpected error closing pool: %v", err)
	}

	// 尝试提交任务
	err = g.Go(func() {})
	if !errors.Is(err, sample.ErrPoolClosed) {
		t.Errorf("Expected ErrPoolClosed, got: %v", err)
	}
}

// TestGoWithFullPool 测试池满的情况
func TestGoWithFullPool(t *testing.T) {
	g := sample.New(
		sample.CorePoolSize(1),
		sample.MaximumPoolSize(1),
		sample.WorkQueue(make(chan func(), 1)),
	)

	// 占满池
	err := g.Go(func() {
		time.Sleep(100 * time.Millisecond)
	})
	if err != nil {
		t.Errorf("Unexpected error submitting first task: %v", err)
	}

	err = g.Go(func() {
		time.Sleep(100 * time.Millisecond)
	})
	if err != nil {
		t.Errorf("Unexpected error submitting second task: %v", err)
	}

	// 尝试提交第三个任务，应该失败
	err = g.Go(func() {})
	if !errors.Is(err, sample.ErrPoolFull) {
		t.Errorf("Expected ErrPoolFull, got: %v", err)
	}
}

// TestGoSuccess 测试成功提交任务
func TestGoSuccess(t *testing.T) {
	g := sample.New()
	var wg sync.WaitGroup
	wg.Add(1)

	err := g.Go(func() {
		defer wg.Done()
		// 任务执行内容
	})
	if err != nil {
		t.Errorf("Unexpected error submitting task: %v", err)
	}

	wg.Wait()
}

// TestCloseWithEmptyPool 测试关闭空池
func TestCloseWithEmptyPool(t *testing.T) {
	g := sample.New()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	err := g.Close(ctx)
	if err != nil {
		t.Errorf("Unexpected error closing empty pool: %v", err)
	}
}

// TestCloseWithRunningTasks 测试关闭有运行任务的池
func TestCloseWithRunningTasks(t *testing.T) {
	g := sample.New()
	var wg sync.WaitGroup
	wg.Add(1)

	// 提交一个长时间运行的任务
	err := g.Go(func() {
		defer wg.Done()
		time.Sleep(100 * time.Millisecond)
	})
	if err != nil {
		t.Errorf("Unexpected error submitting task: %v", err)
	}

	// 启动关闭过程
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	closeErr := make(chan error, 1)
	go func() {
		closeErr <- g.Close(ctx)
	}()

	// 等待任务完成
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		// 任务完成，等待关闭完成
		if err := <-closeErr; err != nil {
			t.Errorf("Unexpected error closing pool: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Error("Timeout waiting for pool to close")
	}
}

// TestCloseWithCancelledContext 测试使用已取消的上下文关闭池
func TestCloseWithCancelledContext(t *testing.T) {
	g := sample.New()

	// 提交一个长时间运行的任务
	err := g.Go(func() {
		time.Sleep(time.Second)
	})
	if err != nil {
		t.Errorf("Unexpected error submitting task: %v", err)
	}

	// 使用已取消的上下文关闭池
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // 立即取消

	err = g.Close(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Errorf("Expected context.Canceled, got: %v", err)
	}
}

// TestCloseTwice 测试多次关闭池
func TestCloseTwice(t *testing.T) {
	g := sample.New()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	// 第一次关闭
	err := g.Close(ctx)
	if err != nil {
		t.Errorf("Unexpected error on first close: %v", err)
	}

	// 第二次关闭
	err = g.Close(ctx)
	if !errors.Is(err, sample.ErrPoolClosed) {
		t.Errorf("Expected ErrPoolClosed on second close, got: %v", err)
	}
}

// TestWorkerLifecycle 测试工作线程生命周期
func TestWorkerLifecycle(t *testing.T) {
	g := sample.New(
		sample.CorePoolSize(2),
		sample.MaximumPoolSize(4),
		sample.KeepAliveTime(50*time.Millisecond),
	)

	// 提交任务
	var wg sync.WaitGroup
	for i := 0; i < 6; i++ {
		wg.Add(1)
		err := g.Go(func() {
			defer wg.Done()
			time.Sleep(20 * time.Millisecond)
		})
		if err != nil {
			t.Errorf("Unexpected error submitting task %d: %v", i, err)
		}
	}

	// 等待所有任务完成
	wg.Wait()

	// 关闭池
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	err := g.Close(ctx)
	if err != nil {
		t.Errorf("Unexpected error closing pool: %v", err)
	}
}

// TestPanicRecovery 测试panic恢复机制
func TestPanicRecovery(t *testing.T) {
	var capturedPanic any
	var capturedStack []byte

	g := sample.New(
		sample.Recover(func(p any, stack []byte) {
			capturedPanic = p
			capturedStack = stack
		}),
	)

	err := g.Go(func() {
		panic("test panic")
	})
	if err != nil {
		t.Errorf("Unexpected error submitting task: %v", err)
	}

	// 等待一段时间让任务执行
	time.Sleep(100 * time.Millisecond)

	// 检查panic是否被捕获
	if capturedPanic == nil {
		t.Error("Expected panic to be captured")
	}

	if len(capturedStack) == 0 {
		t.Error("Expected stack trace to be captured")
	}

	// 关闭池
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	err = g.Close(ctx)
	if err != nil {
		t.Errorf("Unexpected error closing pool: %v", err)
	}
}

// TestCoreAndNonCoreWorkers 测试核心和非核心工作线程的行为
func TestCoreAndNonCoreWorkers(t *testing.T) {
	g := sample.New(
		sample.CorePoolSize(2),
		sample.MaximumPoolSize(4),
		sample.KeepAliveTime(100*time.Millisecond),
	)

	// 提交任务，只使用核心线程
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		err := g.Go(func() {
			defer wg.Done()
			time.Sleep(50 * time.Millisecond)
		})
		if err != nil {
			t.Errorf("Unexpected error submitting core task %d: %v", i, err)
		}
	}

	// 等待核心任务完成
	wg.Wait()

	// 提交更多任务，触发非核心线程创建
	for i := 0; i < 4; i++ {
		wg.Add(1)
		err := g.Go(func() {
			defer wg.Done()
			time.Sleep(50 * time.Millisecond)
		})
		if err != nil {
			t.Errorf("Unexpected error submitting non-core task %d: %v", i, err)
		}
	}

	// 等待所有任务完成
	wg.Wait()

	// 关闭池
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	err := g.Close(ctx)
	if err != nil {
		t.Errorf("Unexpected error closing pool: %v", err)
	}
}

// BenchmarkGo 测试Go方法的性能
func BenchmarkGo(b *testing.B) {
	g := sample.New()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		err := g.Go(func() {
			// 空任务
		})
		if err != nil {
			b.Errorf("Unexpected error: %v", err)
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	err := g.Close(ctx)
	if err != nil {
		b.Errorf("Unexpected error closing pool: %v", err)
	}
}

// BenchmarkGoParallel 并行测试Go方法的性能
func BenchmarkGoParallel(b *testing.B) {
	g := sample.New()

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			err := g.Go(func() {
				// 空任务
			})
			if err != nil {
				b.Errorf("Unexpected error: %v", err)
			}
		}
	})

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	err := g.Close(ctx)
	if err != nil {
		b.Errorf("Unexpected error closing pool: %v", err)
	}
}
