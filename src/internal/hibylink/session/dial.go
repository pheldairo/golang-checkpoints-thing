package session

import (
	"net"
	"time"
)

func dial(address string, timeout time.Duration) (net.Conn, error) {
	dialer := net.Dialer{Timeout: timeout, Control: tuneForLossyLinks}
	return dialer.Dial("tcp", address)
}
