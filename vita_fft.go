package flexclient

import "encoding/binary"

// FFTFrame is a fully assembled FFT spectrum frame delivered by SetFFTChan.
// Data contains TotalBins uint16 values (pre-scaled Y pixel coordinates).
type FFTFrame struct {
	StreamID   uint32
	TotalBins  uint16
	FrameIndex uint32
	Data       []uint16
}

// fftSegBuf accumulates FFT segments for one stream until a complete frame is ready.
type fftSegBuf struct {
	frameIndex  uint32
	totalBins   uint16
	data        []uint16
	accumulated int
}

// SetFFTChan registers a channel to receive fully assembled FFT frames.
// flexclient buffers segments internally until a complete frame is ready.
// The channel is NOT closed by flexclient; callers must manage lifetime.
func (f *FlexClient) SetFFTChan(ch chan FFTFrame) {
	f.Lock()
	defer f.Unlock()
	f.fftChan = ch
}

// parseFFTPacket parses an FFT payload, accumulates segments, and emits complete
// frames to fftChan when all bins are received.
// Must NOT be called with the FlexClient lock held.
func (f *FlexClient) parseFFTPacket(streamID uint32, payload []byte) {
	f.RLock()
	ch := f.fftChan
	f.RUnlock()
	if ch == nil {
		return
	}

	if len(payload) < 12 {
		return
	}

	startBin := binary.BigEndian.Uint16(payload[0:2])
	numBins := binary.BigEndian.Uint16(payload[2:4])
	// payload[4:6] = bin_size (always 2) — skip
	totalBins := binary.BigEndian.Uint16(payload[6:8])
	frameIndex := binary.BigEndian.Uint32(payload[8:12])

	expectedLen := 12 + int(numBins)*2
	if len(payload) < expectedLen || numBins == 0 || totalBins == 0 {
		return
	}

	f.fftMu.Lock()
	if f.fftBufs == nil {
		f.fftBufs = make(map[uint32]*fftSegBuf)
	}
	buf := f.fftBufs[streamID]
	if buf == nil || buf.frameIndex != frameIndex || buf.totalBins != totalBins {
		buf = &fftSegBuf{
			frameIndex: frameIndex,
			totalBins:  totalBins,
			data:       make([]uint16, totalBins),
		}
		f.fftBufs[streamID] = buf
	}

	// Copy segment data into the correct position.
	for i := 0; i < int(numBins); i++ {
		idx := int(startBin) + i
		if idx >= int(totalBins) {
			break
		}
		buf.data[idx] = binary.BigEndian.Uint16(payload[12+i*2 : 14+i*2])
	}
	buf.accumulated += int(numBins)

	if buf.accumulated < int(totalBins) {
		f.fftMu.Unlock()
		return
	}

	// Complete frame — copy data and reset accumulator before sending.
	complete := make([]uint16, totalBins)
	copy(complete, buf.data)
	frame := FFTFrame{
		StreamID:   streamID,
		TotalBins:  totalBins,
		FrameIndex: frameIndex,
		Data:       complete,
	}
	buf.accumulated = 0
	f.fftMu.Unlock()

	select {
	case ch <- frame:
	default:
	}
}

