package flexclient

import (
	"fmt"
	"net"
	"strings"

	"github.com/hb9fxq/flexlib-go/vita"
)

func Discover(specString string) (map[string]string, error) {
	spec, err := parseKv(specString)
	if err != nil {
		return nil, fmt.Errorf("parse discovery spec: %w", err)
	}

	sock, err := discoveryListen()
	if err != nil {
		return nil, fmt.Errorf("listen for discovery packets: %w", err)
	}

	defer sock.Close()

	for {
		pkt := discoveryRecv(sock)
		if discoveryMatch(pkt, spec) {
			return pkt, nil
		}
	}
}

func parseKv(in string) (map[string]string, error) {
	out := map[string]string{}

	in = strings.Trim(in, " \x00")
	parts := strings.Split(in, " ")
	for _, part := range parts {
		if part == "" {
			continue
		}
		eqIdx := strings.IndexByte(part, '=')
		if eqIdx == -1 {
			return nil, fmt.Errorf("couldn't parse key/value pair %s", part)
		}
		key := part[0:eqIdx]
		val := part[eqIdx+1:]
		out[key] = val
	}

	return out, nil
}

func discoveryRecv(conn *net.UDPConn) map[string]string {
	for {
		var pkt [64000]byte
		n, err := conn.Read(pkt[:])
		if err == nil {
			err, preamble, payload := vita.ParseVitaPreamble(pkt[:n])
			if err == nil && preamble.Class_id.OUI == 0x001c2d && preamble.Class_id.PacketClassCode == 0xffff {
				kv, err := parseKv(string(payload))
				if err == nil {
					return kv
				}
			}
		}
	}
}

func discoveryMatch(pkt, spec map[string]string) bool {
	for key, val := range spec {
		if pkt[key] != val {
			return false
		}
	}
	return true
}
