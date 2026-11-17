package machine

import "golang.org/x/sys/unix"

// Sample reads the disk holding dir and the Mac's memory. Each part is read on its own: one that fails is left out.
func Sample(dir string) State {
	var state State
	var stat unix.Statfs_t
	if err := unix.Statfs(dir, &stat); err == nil {
		block := int64(stat.Bsize)
		state.Disk = newDisk(int64(stat.Bavail)*block, int64(stat.Blocks)*block)
	}
	total, errTotal := unix.SysctlUint64("hw.memsize")
	free, errFree := unix.SysctlUint32("kern.memorystatus_level")
	pressure, errPressure := unix.SysctlUint32("kern.memorystatus_vm_pressure_level")
	if errTotal == nil && errFree == nil && errPressure == nil {
		state.Memory = newMemory(free, pressure, int64(total))
	}
	return state
}
