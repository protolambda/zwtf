package events

import (
	"encoding/json"
	"errors"
	. "github.com/protolambda/zrnt/eth2/beacon/attestations"
	. "github.com/protolambda/zrnt/eth2/core"
	. "github.com/protolambda/zrnt/eth2/phase0"
	"zwtf/events/decode"
)

type BeaconHeadChanged struct {
	Reorg                      bool
	CurrentHeadBeaconBlockRoot Root
	PurrentHeadBeaconBlockRoot Root
}

type BeaconFinalization struct {
	Epoch Epoch
	Root  Root
}

type BeaconBlockImported struct {
	BlockRoot Root
	Block     *BeaconBlock
}

type BeaconBlockRejected struct {
	Reason string
	Block  *BeaconBlock
}

type BeaconAttestationImported struct {
	Attestation *Attestation
}

type BeaconAttestationRejected struct {
	Reason      string
	Attestation *Attestation
}

type Event struct {
	Event string
	Data interface{}
}

type EvAlloc func() interface{}

var evTypes = map[string]EvAlloc{
	"beacon_head_changed": func() interface{} { return new(BeaconHeadChanged) },
	"beacon_finalization": func() interface{} { return new(BeaconFinalization) },
	"beacon_block_imported": func() interface{} { return new(BeaconBlockImported) },
	"beacon_block_rejected": func() interface{} { return new(BeaconBlockRejected) },
	"beacon_attestation_imported": func() interface{} { return new(BeaconAttestationImported) },
	"beacon_attestation_rejected": func() interface{} { return new(BeaconAttestationRejected) },
}

func (ev *Event) UnmarshalJSON(data []byte) error {
	var eventData map[string]interface{}
	if err := json.Unmarshal(data, &eventData); err != nil {
		return err
	}
	evType, ok := eventData["event"].(string)
	if !ok {
		return errors.New("cannot read event type")
	}
	evData, ok := eventData["data"]
	if !ok {
		return errors.New("event has no data")
	}
	evAlloc, ok := evTypes[evType]
	if !ok {
		return errors.New("type "+evType+" is not supported")
	}
	evDst := evAlloc()
	if err := decode.DecodeJsonApiData(evDst, evData); err != nil {
		return err
	}
	ev.Event = evType
	ev.Data = evDst
	return nil
}
