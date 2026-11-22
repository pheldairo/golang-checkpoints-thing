//go:build !linux

package session

import "syscall"

func tuneForLossyLinks(network, address string, conn syscall.RawConn) error {
	return nil
}
