package flexclient

import (
	"encoding/binary"
	"math"
)

// SetPCMChan registers a channel to receive float32 mono samples from
// an uncompressed remote_audio_rx stream (class 0x03E3 / IF Narrow).
// The radio sends dual-mono interleaved big-endian float32; flexclient
// delivers the left channel only (L == R for remote_audio_rx).
// The channel is NOT closed by flexclient.
func (f *FlexClient) SetPCMChan(ch chan []float32) {
	f.Lock()
	defer f.Unlock()
	f.pcmChan = ch
}

// parsePCMPacket parses an IF Narrow (PCM) payload and delivers mono float32
// samples to pcmChan. The radio sends big-endian float32 interleaved stereo;
// we deliver the left channel only (L == R for remote_audio_rx).
// Must NOT be called with the FlexClient lock held.
func (f *FlexClient) parsePCMPacket(payload []byte) {
	f.RLock()
	ch := f.pcmChan
	f.RUnlock()
	if ch == nil {
		return
	}

	if len(payload) < 4 || len(payload)%4 != 0 {
		return
	}

	// Big-endian float32 interleaved stereo → left channel mono.
	// Each stereo pair = 8 bytes: [L0..L3][R0..R3]
	nSamples := len(payload) / 4
	nMono := nSamples / 2
	mono := make([]float32, nMono)
	for i := range mono {
		raw := binary.BigEndian.Uint32(payload[i*8 : i*8+4])
		mono[i] = math.Float32frombits(raw)
	}

	select {
	case ch <- mono:
	default:
	}
}
