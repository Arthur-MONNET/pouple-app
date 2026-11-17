package machine

import (
	"context"
	"math"
	"sync"
	"time"
)

// Monitor keeps the last reading of the machine, so a snapshot never waits on the system, and says when it changed
// enough for the screens to show it.
type Monitor struct {
	dir    string
	sample func(string) State
	mu     sync.Mutex
	state  State
}

// NewMonitor reads the machine once, so the first snapshot already has it.
func NewMonitor(dir string) *Monitor {
	return newMonitor(dir, Sample)
}

func newMonitor(dir string, sample func(string) State) *Monitor {
	return &Monitor{dir: dir, sample: sample, state: sample(dir)}
}

// Current is the last reading.
func (m *Monitor) Current() State {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.state
}

// Refresh reads the machine now and reports whether what the screens show of it changed.
func (m *Monitor) Refresh() bool {
	next := m.sample(m.dir)
	m.mu.Lock()
	defer m.mu.Unlock()
	changed := shown(next) != shown(m.state)
	m.state = next
	return changed
}

// Run reads the machine every interval until ctx ends, and calls onChange when what the screens show changed.
func (m *Monitor) Run(ctx context.Context, interval time.Duration, onChange func()) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if m.Refresh() {
				onChange()
			}
		}
	}
}

// view is a reading at the precision the screens show it: a tenth of a gigabyte, a whole percent.
type view struct {
	disk, memory           bool
	freeTenths, memoryUsed int64
	diskLevel, memoryLevel Level
}

func shown(state State) view {
	var v view
	if state.Disk != nil {
		v.disk, v.diskLevel = true, state.Disk.Level
		v.freeTenths = int64(math.Round(float64(state.Disk.FreeBytes) / float64(gib) * 10))
	}
	if state.Memory != nil {
		v.memory, v.memoryLevel = true, state.Memory.Level
		v.memoryUsed = int64(math.Round(state.Memory.UsedPercent))
	}
	return v
}
