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
	freqStr := fmt.Sprintf("%.6f", freq)
	return f.sendAndUpdateObj(
		ctx,
		"slice t "+sliceIdx+" "+freqStr,
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
