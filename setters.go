package flexclient

import (
	"fmt"
	"strconv"
)

func (f *FlexClient) sendAndUpdateObj(cmd, object string, changes Object) CmdResult {
	// TODO: do a dance with "locking" updateState somehow so that we can get the
	// cmdresult without any intermediate notifications being applied, then apply
	// our changes, then unlock updateState. That way if anything the radio sends
	// back contradicts the changes provided, the radio will win.
	res := f.SendAndWait(cmd)

	if res.Error == 0 {
		f.Lock()
		defer f.Unlock()
		f.updateState("", object, changes)
	}

	return res
}

func (f *FlexClient) setAndUpdateObj(cmdPrefix, object string, changes Object) CmdResult {
	cmd := cmdPrefix
	for k, v := range changes {
		// TODO: validate k and v are legal (no spaces or equals)
		cmd += " " + k + "=" + v
	}

	return f.sendAndUpdateObj(cmd, object, changes)
}

func (f *FlexClient) SliceSet(sliceIdx string, values Object) CmdResult {
	return f.setAndUpdateObj("slice set "+sliceIdx, "slice "+sliceIdx, values)
}

func (f *FlexClient) SliceTune(sliceIdx string, freq float64) CmdResult {
	freqStr := fmt.Sprintf("%.6f", freq)
	return f.sendAndUpdateObj(
		"slice t "+sliceIdx+" "+freqStr,
		"slice "+sliceIdx,
		Object{"RF_frequency": freqStr},
	)
}

func (f *FlexClient) SliceSetFilter(sliceIdx string, filterLo, filterHi int) CmdResult {
	lo := strconv.Itoa(filterLo)
	hi := strconv.Itoa(filterHi)
	return f.sendAndUpdateObj(
		"filt "+sliceIdx+" "+lo+" "+hi,
		"slice "+sliceIdx,
		Object{"filter_lo": lo, "filter_hi": hi},
	)
}

func (f *FlexClient) TransmitSet(values Object) CmdResult {
	return f.setAndUpdateObj("transmit set", "transmit", values)
}

func (f *FlexClient) TransmitTune(val string) CmdResult {
	return f.sendAndUpdateObj(
		"transmit tune "+val,
		"transmit",
		Object{"tune": val},
	)
}

func (f *FlexClient) RadioSet(values Object) CmdResult {
	return f.setAndUpdateObj("radio set", "radio", values)
}

func (f *FlexClient) PanSet(id string, values Object) CmdResult {
	result := f.setAndUpdateObj("display pan set "+id, "display pan "+id, values)
	if result.Error != 0 {
		return result
	}

	// Funny API quirk: updating a panadapter also updates an associated waterfall
	// but you won't get a notification about it. Synthesize a state update on the
	// waterfall, for any keys that also exist there.
	f.Lock()
	defer f.Unlock()
	pan, ok := f.getObject("display pan " + id)
	if !ok {
		return result
	}
	childWaterfallId, ok := pan["waterfall"]
	if !ok {
		return result
	}
	wf, ok := f.getObject("display waterfall " + childWaterfallId)
	if !ok {
		return result
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
	return result
}
