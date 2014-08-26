// +build darwin

package socket

import "syscall"

var (
	SOCK_CLOEXEC  = 0x0
	SOCK_NONBLOCK = 0x0

	SO_REUSEPORT = syscall.SO_REUSEPORT
)

func maxListenerBacklog() int {
	var (
		n   uint32
		err error
	)
	n, err = syscall.SysctlUint32("kern.ipc.somaxconn")
	if n == 0 || err != nil {
		return syscall.SOMAXCONN
	}
	// FreeBSD stores the backlog in a uint16, as does Linux.
	// Assume the other BSDs do too. Truncate number to avoid wrapping.
	// See issue 5030.
	if n > 1<<16-1 {
		n = 1<<16 - 1
	}
	return int(n)
}
