package fetch

import (
	"bytes"
	"fmt"
	"github.com/protolambda/zrnt/eth2/core"
	"github.com/protolambda/zrnt/eth2/phase0"
	"github.com/protolambda/zssz"
	"net/http"
)

type BeaconAPIFetcher struct {
	client http.Client
	endpoint string
}

type APIBeaconBlock struct {
	Root core.Root
	BeaconBlock phase0.BeaconBlock
}

var APIBeaconBlockSSZ = zssz.GetSSZ((*APIBeaconBlock)(nil))

type APIBeaconState struct {
	Root core.Root
	BeaconState phase0.BeaconState
}

var APIBeaconStateSSZ = zssz.GetSSZ((*APIBeaconState)(nil))

func NewBeaconAPIFetcher(endpoint string) *BeaconAPIFetcher {
	return &BeaconAPIFetcher{
		endpoint: endpoint,
	}
}

func (f *BeaconAPIFetcher) GetStateByBlockRoot(blockRoot core.Root) (*phase0.BeaconState, error) {
	// just naively get the state root of the block, then fetch the state.
	// TODO: maybe improve this later, if the available API matures
	block, err := f.GetBlockByBlockRoot(blockRoot)
	if err != nil {
		return nil, err
	}
	state, err := f.GetState(block.StateRoot)
	if err != nil {
		return nil, err
	}
	return state, err
}

func (f *BeaconAPIFetcher) GetBlockByBlockRoot(blockRoot core.Root) (*phase0.BeaconBlock, error) {
	req, err := http.NewRequest("GET", f.endpoint + fmt.Sprintf("/beacon/block?root=0x%x", blockRoot), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Add("Accept", "application/ssz")
	resp, err := f.client.Do(req)
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	defer resp.Body.Close()
	if _, err := buf.ReadFrom(resp.Body); err != nil {
		return nil, err
	}
	var out APIBeaconBlock
	if err := zssz.Decode(&buf, uint64(buf.Len()), &out, APIBeaconBlockSSZ); err != nil {
		return nil, err
	}
	return &out.BeaconBlock, nil
}

func (f *BeaconAPIFetcher) GetState(stateRoot core.Root) (*phase0.BeaconState, error) {
	req, err := http.NewRequest("GET", f.endpoint + fmt.Sprintf("/beacon/state?root=0x%x", stateRoot), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Add("Accept", "application/ssz")
	resp, err := f.client.Do(req)
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	defer resp.Body.Close()
	if _, err := buf.ReadFrom(resp.Body); err != nil {
		return nil, err
	}
	var out APIBeaconState
	if err := zssz.Decode(&buf, uint64(buf.Len()), &out, APIBeaconStateSSZ); err != nil {
		return nil, err
	}
	return &out.BeaconState, nil
}
