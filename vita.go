package flexclient

import (
	"log"

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
		log.Printf("flexclient: VITA parse error: %v", err)
	}
}

func (f *FlexClient) SendUdp(pkt []byte) error {
	// If udpDest is nil, this is a pre-connected socket (from InitWanUDP)
	// and we must use Write() instead of WriteTo()
	if f.udpDest == nil {
		_, err := f.udpConn.Write(pkt)
		return err
	}
	// Otherwise, this is an unconnected socket (from InitUDP)
	// and we use WriteTo() with the destination
	_, err := f.udpConn.WriteTo(pkt, f.udpDest)
	return err
}

// SetVitaChan sets the channel for receiving raw VITA-49 packets.
// This must be called before InitUDP/StartUDP. The channel will be closed when RunUDP() exits.
// The caller creates the channel but must not close it.
func (f *FlexClient) SetVitaChan(ch chan VitaPacket) {
	f.Lock()
	defer f.Unlock()
	f.vitaPackets = ch
}
