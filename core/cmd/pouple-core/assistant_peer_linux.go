package main

import (
	"os"

	"golang.org/x/sys/unix"
)

func sameAssistantUser(fd int) bool {
	credentials, err := unix.GetsockoptUcred(fd, unix.SOL_SOCKET, unix.SO_PEERCRED)
	return err == nil && credentials.Uid == uint32(os.Geteuid())
}
