package main

import (
	"os"

	"golang.org/x/sys/unix"
)

func sameAssistantUser(fd int) bool {
	credentials, err := unix.GetsockoptXucred(fd, unix.SOL_LOCAL, unix.LOCAL_PEERCRED)
	return err == nil && credentials.Uid == uint32(os.Geteuid())
}
