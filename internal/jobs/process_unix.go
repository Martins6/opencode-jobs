//go:build darwin || linux

package jobs

import "syscall"

// ProcessAlive reports whether pid is a live process.
func ProcessAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	err := syscall.Kill(pid, 0)
	if err == nil {
		return true
	}
	return err == syscall.EPERM
}

func signalProcessGroup(pid int, signal syscall.Signal) {
	if pid <= 0 {
		return
	}
	// The child is a process-group leader (Setpgid), so a negative PID targets
	// the whole group and catches grandchildren.
	_ = syscall.Kill(-pid, signal)
	_ = syscall.Kill(pid, signal)
}
