//go:build linux

package session

import "syscall"

const tcpThinLinearTimeouts = 16

func tuneForLossyLinks(network, address string, conn syscall.RawConn) error {
	return conn.Control(func(fd uintptr) {
		_ = syscall.SetsockoptInt(int(fd), syscall.IPPROTO_TCP, tcpThinLinearTimeouts, 1)
	})
}
