package main

import (
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"syscall"
)

type assistantListener struct{ *net.UnixListener }

func (listener assistantListener) Accept() (net.Conn, error) {
	for {
		conn, err := listener.AcceptUnix()
		if err != nil {
			return nil, err
		}
		raw, err := conn.SyscallConn()
		if err == nil {
			var allowed bool
			if err := raw.Control(func(fd uintptr) { allowed = sameAssistantUser(int(fd)) }); err == nil && allowed {
				return conn, nil
			}
		}
		_ = conn.Close()
	}
}

// openAssistantSocket keeps the socket under a private directory, including on Unix systems
// that don't enforce permission bits on socket files. The lock protects stale socket cleanup.
func openAssistantSocket(home string) (*net.UnixListener, func(), error) {
	if err := checkSocketDirectory(home, 0o022); err != nil {
		return nil, nil, err
	}
	dir := filepath.Join(home, "ipc")
	if err := os.Mkdir(dir, 0o700); err != nil && !errors.Is(err, os.ErrExist) {
		return nil, nil, err
	}
	if err := checkSocketDirectory(dir, 0o077); err != nil {
		return nil, nil, err
	}

	lock, err := os.OpenFile(filepath.Join(dir, "assistant.lock"), os.O_CREATE|os.O_RDWR|syscall.O_NOFOLLOW, 0o600)
	if err != nil {
		return nil, nil, err
	}
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = lock.Close()
		return nil, nil, fmt.Errorf("lock local assistant API: %w", err)
	}
	socket, err := listenAssistantSocket(filepath.Join(dir, "assistant.sock"))
	if err != nil {
		_ = lock.Close()
		return nil, nil, err
	}
	return socket, func() {
		_ = socket.Close()
		_ = lock.Close()
	}, nil
}

func checkSocketDirectory(path string, forbiddenPermissions os.FileMode) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	owner, ok := info.Sys().(*syscall.Stat_t)
	if !info.IsDir() || info.Mode().Perm()&forbiddenPermissions != 0 || !ok || owner.Uid != uint32(os.Geteuid()) {
		return fmt.Errorf("unsafe local assistant API directory: %s", path)
	}
	return nil
}

func listenAssistantSocket(path string) (*net.UnixListener, error) {
	switch info, err := os.Lstat(path); {
	case err == nil:
		if info.Mode().Type() != os.ModeSocket {
			return nil, fmt.Errorf("local assistant API path is occupied: %s", path)
		}
		if err := os.Remove(path); err != nil {
			return nil, err
		}
	case !errors.Is(err, os.ErrNotExist):
		return nil, err
	}
	socket, err := net.ListenUnix("unix", &net.UnixAddr{Name: path, Net: "unix"})
	if err != nil {
		return nil, err
	}
	if err := os.Chmod(path, 0o600); err != nil {
		_ = socket.Close()
		return nil, err
	}
	return socket, nil
}
