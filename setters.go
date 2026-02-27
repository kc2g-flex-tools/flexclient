package flexclient

import (
	"context"
	"fmt"
	"strconv"
)

func (f *FlexClient) sendAndUpdateObj(ctx context.Context, cmd, object string, changes Object) (CmdResult, error) {
	// TODO: do a dance with "locking" updateState somehow so that we can get the
	// cmdresult without any intermediate notifications being applied, then apply
	// our changes, then unlock updateState. That way if anything the radio sends
	// back contradicts the changes provided, the radio will win.
	res, err := f.SendAndWaitContext(ctx, cmd)
	if err != nil {
		return res, err
	}

	if res.Error == 0 {
		f.Lock()
		defer f.Unlock()
		f.updateState("", object, changes)
	}

	return res, nil
}

func (f *FlexClient) setAndUpdateObj(ctx context.Context, cmdPrefix, object string, changes Object) (CmdResult, error) {
	cmd := cmdPrefix
	for k, v := range changes {
		// TODO: validate k and v are legal (no spaces or equals)
		cmd += " " + k + "=" + v
	}

	return f.sendAndUpdateObj(ctx, cmd, object, changes)
}

func (f *FlexClient) SliceSet(ctx context.Context, sliceIdx string, values Object) (CmdResult, error) {
	return f.setAndUpdateObj(ctx, "slice set "+sliceIdx, "slice "+sliceIdx, values)
}

func (f *FlexClient) SliceTune(ctx context.Context, sliceIdx string, freq float64) (CmdResult, error) {
	return f.SliceTuneOpts(ctx, sliceIdx, freq, Object{})
}

func (f *FlexClient) SliceTuneOpts(ctx context.Context, sliceIdx string, freq float64, opts Object) (CmdResult, error) {
	freqStr := fmt.Sprintf("%.6f", freq)
	cmd := "slice t " + sliceIdx + " " + freqStr
	for k, v := range opts {
		cmd += " " + k + "=" + v
	}
	return f.sendAndUpdateObj(
		ctx,
		cmd,
		"slice "+sliceIdx,
		Object{"RF_frequency": freqStr},
	)
}

func (f *FlexClient) SliceSetFilter(ctx context.Context, sliceIdx string, filterLo, filterHi int) (CmdResult, error) {
	lo := strconv.Itoa(filterLo)
	hi := strconv.Itoa(filterHi)
	return f.sendAndUpdateObj(
		ctx,
		"filt "+sliceIdx+" "+lo+" "+hi,
		"slice "+sliceIdx,
		Object{"filter_lo": lo, "filter_hi": hi},
	)
}

func (f *FlexClient) TransmitSet(ctx context.Context, values Object) (CmdResult, error) {
	return f.setAndUpdateObj(ctx, "transmit set", "transmit", values)
}

func (f *FlexClient) TransmitAMCarrierSet(ctx context.Context, val string) (CmdResult, error) {
	return f.sendAndUpdateObj(ctx, "transmit set am_carrier="+val, "transmit", Object{"am_carrier_level": val})
}

func (f *FlexClient) TransmitMicLevelSet(ctx context.Context, val string) (CmdResult, error) {
	return f.sendAndUpdateObj(ctx, "transmit set miclevel="+val, "transmit", Object{"mic_level": val})
}

func (f *FlexClient) TransmitTune(ctx context.Context, val string) (CmdResult, error) {
	return f.sendAndUpdateObj(
		ctx,
		"transmit tune "+val,
		"transmit",
		Object{"tune": val},
	)
}

func (f *FlexClient) RadioSet(ctx context.Context, values Object) (CmdResult, error) {
	return f.setAndUpdateObj(ctx, "radio set", "radio", values)
}

func (f *FlexClient) PanSet(ctx context.Context, id string, values Object) (CmdResult, error) {
	result, err := f.setAndUpdateObj(ctx, "display pan set "+id, "display pan "+id, values)
	if err != nil {
		return result, err
	}

	if result.Error != 0 {
		return result, nil
	}

	// Funny API quirk: updating a panadapter also updates an associated waterfall
	// but you won't get a notification about it. Synthesize a state update on the
	// waterfall, for any keys that also exist there.
	f.Lock()
	defer f.Unlock()
	pan, ok := f.getObject("display pan " + id)
	if !ok {
		return result, fmt.Errorf("pan %s not found", id)
	}
	childWaterfallId, ok := pan["waterfall"]
	if !ok {
		return result, fmt.Errorf("pan %s has no associated waterfall", id)
	}
	wf, ok := f.getObject("display waterfall " + childWaterfallId)
	if !ok {
		return result, fmt.Errorf("waterfall %s not found", childWaterfallId)
	}
	var setWf = values.Copy()
	for key := range values {
		if _, exists := wf[key]; !exists {
			delete(setWf, key)
		}
	}
	if len(setWf) > 0 {
		f.updateState("", "display waterfall "+childWaterfallId, setWf)
	}
	return result, nil
}

func (f *FlexClient) UsbCableSet(ctx context.Context, id string, values Object) (CmdResult, error) {
	return f.setAndUpdateObj(ctx,
		"usb_cable set "+id,
		"usb_cable "+id,
		values,
	)
}

// PanafallCreate creates a panadapter+waterfall display.
// Returns a CmdResult whose Message is "0x<panID>" or "0x<panID>,0x<wfID>".
func (f *FlexClient) PanafallCreate(ctx context.Context, freq float64, xpixels, ypixels int) (CmdResult, error) {
	return f.SendAndWaitContext(ctx, fmt.Sprintf("display pan c freq=%.6f x=%d y=%d", freq, xpixels, ypixels))
}

// WaterfallSet sets display panafall properties (auto_black, color_gain, black_level, etc.)
// id is the waterfall stream ID hex string, e.g. "0x42000001".
func (f *FlexClient) WaterfallSet(ctx context.Context, id string, values Object) (CmdResult, error) {
	return f.setAndUpdateObj(ctx, "display panafall s "+id, "display waterfall "+id, values)
}

// StreamCreate sends "stream create" with the given key=value fields.
func (f *FlexClient) StreamCreate(ctx context.Context, values Object) (CmdResult, error) {
	cmd := "stream create"
	for k, v := range values {
		cmd += " " + k + "=" + v
	}
	return f.SendAndWaitContext(ctx, cmd)
}

// StreamRemove sends "stream remove <id>". id should include the "0x" prefix.
func (f *FlexClient) StreamRemove(ctx context.Context, id string) (CmdResult, error) {
	return f.SendAndWaitContext(ctx, "stream remove "+id)
}

// SliceCreate sends "slice create" with optional fields (freq, mode, ant, pan).
func (f *FlexClient) SliceCreate(ctx context.Context, values Object) (CmdResult, error) {
	cmd := "slice create"
	for k, v := range values {
		cmd += " " + k + "=" + v
	}
	return f.SendAndWaitContext(ctx, cmd)
}

// SliceRemove sends "slice remove <idx>".
func (f *FlexClient) SliceRemove(ctx context.Context, sliceIdx string) (CmdResult, error) {
	return f.SendAndWaitContext(ctx, "slice r "+sliceIdx)
}

// SliceAudioGain sets the audio gain for a slice on a given client handle.
// gain is in the range 0.0–1.0. clientHandle should be a hex string like "0x12345678".
// Optimistically updates the slice's audio_level state (0–100 integer).
func (f *FlexClient) SliceAudioGain(ctx context.Context, clientHandle, sliceIdx string, gain float64) (CmdResult, error) {
	return f.sendAndUpdateObj(ctx,
		fmt.Sprintf("audio client %s slice %s gain %.2f", clientHandle, sliceIdx, gain),
		"slice "+sliceIdx,
		Object{"audio_level": strconv.Itoa(int(gain*100))},
	)
}

// SliceAudioMute sets the mute state for a slice on a given client handle.
// clientHandle should be a hex string like "0x12345678".
// Optimistically updates the slice's audio_mute state.
func (f *FlexClient) SliceAudioMute(ctx context.Context, clientHandle, sliceIdx string, mute bool) (CmdResult, error) {
	muteStr := "0"
	if mute {
		muteStr = "1"
	}
	return f.sendAndUpdateObj(ctx,
		fmt.Sprintf("audio client %s slice %s mute %s", clientHandle, sliceIdx, muteStr),
		"slice "+sliceIdx,
		Object{"audio_mute": muteStr},
	)
}
