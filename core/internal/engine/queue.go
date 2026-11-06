package engine

import "sync"

// Priority orders jobs: what the user asked for goes before what runs on its own in the background.
type Priority int

const (
	Interactive Priority = iota
	Background
)

// Queue runs jobs one at a time (AI turns are heavy), interactive jobs first, each priority in arrival order.
type Queue struct {
	mu      sync.Mutex
	lanes   [2][]func()
	running bool
	idle    *sync.Cond
}

func NewQueue() *Queue {
	q := &Queue{}
	q.idle = sync.NewCond(&q.mu)
	return q
}

// Add schedules a job.
func (q *Queue) Add(priority Priority, job func()) {
	q.mu.Lock()
	q.lanes[priority] = append(q.lanes[priority], job)
	start := !q.running
	q.running = true
	q.mu.Unlock()
	if start {
		go q.drain()
	}
}

func (q *Queue) next() (func(), bool) {
	q.mu.Lock()
	defer q.mu.Unlock()
	for i := range q.lanes {
		if len(q.lanes[i]) > 0 {
			job := q.lanes[i][0]
			q.lanes[i] = q.lanes[i][1:]
			return job, true
		}
	}
	q.running = false
	q.idle.Broadcast()
	return nil, false
}

func (q *Queue) drain() {
	for {
		job, ok := q.next()
		if !ok {
			return
		}
		job()
	}
}

// Wait blocks until nothing is queued or running.
func (q *Queue) Wait() {
	q.mu.Lock()
	defer q.mu.Unlock()
	for q.running {
		q.idle.Wait()
	}
}
