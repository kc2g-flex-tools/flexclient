package flexclient

import "fmt"

func (f *FlexClient) sendAndUpdateObj(cmd, object string, changes Object) CmdResult {
	// TODO: do a dance with "locking" updateState somehow so that we can get the
	// cmdresult without any intermediate notifications being applied, then apply
	// our changes, then unlock updateState. That way if anything the radio sends
	// back contradicts the changes provided, the radio will win.
	f.Lock()
	defer f.Unlock()
	res := f.SendAndWait(cmd)

	if res.Error == 0 {
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

func (f *FlexClient) TransmitSet(values Object) CmdResult {
	return f.setAndUpdateObj("transmit", "transmit", values)
}

func (f *FlexClient) RadioSet(values Object) CmdResult {
	return f.setAndUpdateObj("radio set", "radio", values)
}
