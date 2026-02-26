package flexclient

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/hb9fxq/flexlib-go/vita"
)

// MeterReport represents a meter reading. It combines data from
// a UDP meter packet with metadata from a meter subscription.
type MeterReport struct {
	// ID is the numeric meter identifier from the VITA-49 packet.
	ID uint16
	// Source is the source of the meter (e.g. "TX-", "RAD", or "SLC")
	Source string
	// Name is the name of the meter
	Name string
	// Num is an int associated with the source, e.g. if Source is "SLC" then Num is the slice index.
	Num int
	// Unit is the unit of measurement (e.g., "dBm", "Volts", "degF")
	Unit string
	// Low is the minimum of the valid range
	Low float64
	// High is the maximum of the valid range
	High float64
	// RawValue is the raw value from the radio
	RawValue int16
	// Value is the translated value based on the unit
	Value float64
}

func (f *FlexClient) parseMeterState(handle, meterState string) {
	if strings.Contains(meterState, "removed") {
		meterId, _, _ := strings.Cut(meterState, " ")
		objName := "meter " + meterId
		f.state[objName] = nil
		f.updateState(handle, objName, Object{})
		delete(f.meterTemplates, objName)
	} else {
		props := strings.Split(meterState, "#")
		meters := map[string]Object{}
		for _, prop := range props {
			key, val, ok := strings.Cut(prop, "=")
			if !ok {
				continue
			}
			meterId, key, _ := strings.Cut(key, ".")
			if _, ok := meters[meterId]; !ok {
				meters[meterId] = Object{}
			}
			meters[meterId][key] = val
		}

		for meterId, obj := range meters {
			objName := "meter " + meterId
			f.state[objName] = obj
			f.updateState(handle, objName, obj)

			var numInt int
			var err error
			if obj["num"] != "" {
				numInt, err = strconv.Atoi(obj["num"])
				if err != nil {
					numInt = 0
				}
			}

			lowVal := 0.0
			highVal := 0.0
			if obj["low"] != "" {
				if v, err := strconv.ParseFloat(obj["low"], 64); err == nil {
					lowVal = v
				}
			}
			if obj["hi"] != "" {
				if v, err := strconv.ParseFloat(obj["hi"], 64); err == nil {
					highVal = v
				}
			}

			f.meterTemplates[objName] = MeterReport{
				Source: obj["src"],
				Name:   obj["nam"],
				Num:    numInt,
				Unit:   obj["unit"],
				Low:    lowVal,
				High:   highVal,
			}
		}
	}
}

// translateMeterValue converts the raw meter value to a float64 based on the unit
func translateMeterValue(rawValue int16, unit string) float64 {
	switch unit {
	case "dBm", "dBFS", "dB", "SWR":
		return float64(rawValue) / 128
	case "Volts":
		return float64(rawValue) / 256
	case "degF", "degC":
		return float64(rawValue) / 64
	default:
		return float64(rawValue)
	}
}

// SetMeterChan sets a channel to receive meter reports
func (f *FlexClient) SetMeterChan(ch chan MeterReport) {
	f.Lock()
	defer f.Unlock()
	f.meterChan = ch
}

// processMeterPacket processes a VITA meter packet and sends reports to the meter channel if set
func (f *FlexClient) processMeterPacket(payload []byte) {
	f.RLock()
	meterChan := f.meterChan
	f.RUnlock()

	if meterChan == nil {
		// No need to spend the work processing if no channel is set
		return
	}

	reader := bytes.NewReader(payload)
	for {
		var id uint16
		var rawVal int16

		err := binary.Read(reader, binary.BigEndian, &id)
		if err == io.EOF {
			return
		} else if err != nil {
			return
		}

		err = binary.Read(reader, binary.BigEndian, &rawVal)
		if err != nil {
			return
		}

		// Get meter info from state
		meterName := fmt.Sprintf("meter %d", id)
		f.RLock()
		meterTemplate, exists := f.meterTemplates[meterName]
		f.RUnlock()

		if exists {
			report := meterTemplate
			report.ID = id
			report.RawValue = rawVal
			report.Value = translateMeterValue(rawVal, report.Unit)

			select {
			case meterChan <- report:
			default:
				// Channel is full or closed, continue without blocking
			}
		}
	}
}

// isMeterPacket returns true if the VITA packet is a meter packet
func isMeterPacket(preamble *vita.VitaPacketPreamble) bool {
	// Meter packets are recognized by:
	// - OUI: 0x001c2d
	// - Information class: 0x534c ("SL")
	// - Packet class: 0x8002
	return preamble.Class_id.OUI == 0x001c2d &&
		preamble.Class_id.InformationClassCode == 0x534c &&
		preamble.Class_id.PacketClassCode == 0x8002
}
