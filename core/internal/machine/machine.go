// Package machine reads how much disk and memory the Mac has left. Pouple shows it next to the AIs' usage, and reads
// it again when a turn fails: a full disk or a Mac out of memory makes the AIs fail with errors that never say so.
package machine

// Level is how tight a resource is, with the same three steps as the AIs' usage windows.
type Level string

const (
	Normal   Level = "normal"
	High     Level = "high"
	Critical Level = "critical"
)

const gib = int64(1) << 30

// Below these, the AIs start failing: they write their conversations, logs and caches on the disk at every turn.
const (
	diskCritical = 2 * gib
	diskHigh     = 10 * gib
)

// Disk is the volume holding Pouple's data, where the AIs keep their conversations.
type Disk struct {
	FreeBytes  int64 `json:"freeBytes"`
	TotalBytes int64 `json:"totalBytes"`
	Level      Level `json:"level"`
}

// UsedPercent is the share of the volume in use, from 0 to 100.
func (d Disk) UsedPercent() float64 {
	if d.TotalBytes <= 0 {
		return 0
	}
	return float64(d.TotalBytes-d.FreeBytes) / float64(d.TotalBytes) * 100
}

// Memory is the Mac's memory as the system judges it: its level is macOS's own memory pressure, not a share in use,
// since macOS keeps memory full of caches it gives back on demand.
type Memory struct {
	// UsedPercent is the share the system cannot give back at once, from 0 to 100.
	UsedPercent float64 `json:"usedPercent"`
	TotalBytes  int64   `json:"totalBytes"`
	Level       Level   `json:"level"`
}

// State is one reading; a part that could not be read is absent.
type State struct {
	Disk   *Disk   `json:"disk,omitempty"`
	Memory *Memory `json:"memory,omitempty"`
}

// diskLevel judges the free space in bytes, not as a share: a few gigabytes are as short on a large disk as on a small one.
func diskLevel(free int64) Level {
	switch {
	case free < diskCritical:
		return Critical
	case free < diskHigh:
		return High
	}
	return Normal
}

// pressureLevel maps macOS's memory pressure (kern.memorystatus_vm_pressure_level: 1 normal, 2 warning, 4 critical).
func pressureLevel(pressure uint32) Level {
	switch {
	case pressure >= 4:
		return Critical
	case pressure >= 2:
		return High
	}
	return Normal
}

func newDisk(free, total int64) *Disk {
	return &Disk{FreeBytes: free, TotalBytes: total, Level: diskLevel(free)}
}

func newMemory(freePercent, pressure uint32, total int64) *Memory {
	used := 100 - float64(min(freePercent, 100))
	return &Memory{UsedPercent: used, TotalBytes: total, Level: pressureLevel(pressure)}
}
