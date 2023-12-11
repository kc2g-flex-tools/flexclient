//go:build !unix

package flexclient

import (
	"fmt"
	"net"
)

func discoveryListen() (*net.UDPConn, error) {
	conn, err := net.ListenPacket("udp", ":4992")
	if err != nil {
		return nil, fmt.Errorf("discoveryListen: %w", err)
	}
	return conn.(*net.UDPConn), nil
}
