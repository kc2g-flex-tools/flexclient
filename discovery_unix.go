//go:build unix

package flexclient

import (
	"context"
	"fmt"
	"net"
	"syscall"

	"golang.org/x/sys/unix"
)

func discoveryListen() (*net.UDPConn, error) {
	lc := net.ListenConfig{
		Control: func(_, _ string, c syscall.RawConn) error {
			var opErr error
			err := c.Control(func(fd uintptr) {
				opErr = unix.SetsockoptInt(int(fd), unix.SOL_SOCKET, unix.SO_REUSEPORT, 1)
			})
			if err != nil {
				return err
			}
			return opErr
		},
	}

	conn, err := lc.ListenPacket(context.Background(), "udp", ":4992")
	if err != nil {
		return nil, fmt.Errorf("discoveryListen: %w", err)
	}
	return conn.(*net.UDPConn), nil
}
