package flexclient

import (
	"fmt"
	"os"

	"github.com/hb9fxq/flexlib-go/vita"
)

type VitaPacket struct {
	Preamble *vita.VitaPacketPreamble
	Payload  []byte
}

func (f *FlexClient) parseUDP(pkt []byte) {
	f.RLock()
	dchan := f.vitaPackets
	f.RUnlock()

	err, preamble, payloadTmp := vita.ParseVitaPreamble(pkt)
	if err == nil {
		payload := make([]byte, len(payloadTmp))
		copy(payload, payloadTmp)

		// Check if this is a meter packet
		if isMeterPacket(preamble) {
			// Process meter packet
			f.processMeterPacket(payload)
		}

		// Forward the packet to vitaPackets channel if set
		if dchan != nil {
			select {
			case dchan <- VitaPacket{preamble, payload}:
			default:
			}
		}
	} else {
		fmt.Fprintf(os.Stderr, "vita parse err %s\n", err.Error())
	}
}

func (f *FlexClient) SendUdp(pkt []byte) error {
	_, err := f.udpConn.WriteTo(pkt, f.udpDest)
	return err
}

func (f *FlexClient) SetVitaChan(ch chan VitaPacket) {
	f.Lock()
	defer f.Unlock()
	f.vitaPackets = ch
}
