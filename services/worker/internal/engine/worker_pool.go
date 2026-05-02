package engine

import (
	"context"
	"log/slog"
	"sync"
)

// WorkerPool manages a fixed pool of goroutines that drain the JobQueue.
type WorkerPool struct {
	JobQueue chan Job // buffered channel — producers send jobs here
	workers  int
	executor *Executor
	log      *slog.Logger
	wg       sync.WaitGroup
}

// NewWorkerPool creates a WorkerPool with a buffered job channel of capacity 100.
func NewWorkerPool(maxWorkers int, executor *Executor, log *slog.Logger) *WorkerPool {
	return &WorkerPool{
		JobQueue: make(chan Job, 100),
		workers:  maxWorkers,
		executor: executor,
		log:      log,
	}
}

// Start spawns maxWorkers goroutines that drain JobQueue until ctx is cancelled.
func (p *WorkerPool) Start(ctx context.Context) {
	for i := 0; i < p.workers; i++ {
		p.wg.Add(1)
		go p.worker(ctx, i)
	}
	p.log.Info("worker pool started", "workers", p.workers)
}

// Shutdown waits for all in-flight jobs to finish.
// Call after stopping the NATS consumer and cron scheduler so no new jobs arrive.
func (p *WorkerPool) Shutdown() {
	close(p.JobQueue)
	p.wg.Wait()
	p.log.Info("worker pool shut down")
}

// worker is the goroutine body. It drains JobQueue until closed or ctx is done.
func (p *WorkerPool) worker(ctx context.Context, id int) {
	defer p.wg.Done()
	p.log.Debug("worker started", "worker_id", id)

	for {
		select {
		case <-ctx.Done():
			return
		case job, ok := <-p.JobQueue:
			if !ok {
				// Channel closed — drain complete
				return
			}
			p.executor.Process(ctx, job)
		}
	}
}
