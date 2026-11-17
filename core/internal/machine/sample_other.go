//go:build !darwin

package machine

// Sample reads nothing outside macOS, the only system Pouple runs on: the interfaces then show no machine line.
func Sample(string) State { return State{} }
