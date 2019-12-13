package memory

import (
	"errors"
	"fmt"
	"github.com/protolambda/zrnt/eth2/beacon/seeding"
	"github.com/protolambda/zrnt/eth2/beacon/shuffling"
	. "github.com/protolambda/zrnt/eth2/core"
	"github.com/protolambda/zrnt/eth2/phase0"
	"zwtf/events"
)

const HeadsMemory = 1000
const FinalizedMemory = 1000
const BlocksMemory = 1000
const AttestationsMemory = 1000
const LatestVotesMemory = 10000

type BlockPtr uint32
type AttestationPtr uint32
type LatestVotesPtr uint32

type BlockSummary struct {
	HTR    Root
	Slot   Slot
	Parent BlockPtr
}

type AttestationSummary struct {
	Slot      Slot
	CommIndex CommitteeIndex
	Head      BlockPtr
	Target    BlockPtr
	Source    BlockPtr
}

type VoteSummary struct {
	ValidatorIndex ValidatorIndex
	AttestationPtr AttestationPtr
}

type MemoryState struct {
	// ptrs rotate around buffer
	HeadNextPtr         BlockPtr       // index modulo HeadsMemory
	FinalizedNextPtr    BlockPtr       // index modulo FinalizedMemory
	BlocksNextPtr       BlockPtr       // index modulo BlocksMemory
	AttestationsNextPtr AttestationPtr // index modulo AttestationsMemory
	LatestVotesNextPtr  LatestVotesPtr // index modulo LatestVotesMemory
}

type Memory struct {
	MemoryState
	HeadBuffer         [HeadsMemory]BlockPtr
	FinalizedBuffer    [FinalizedMemory]BlockPtr
	BlocksBuffer       [BlocksMemory]BlockSummary
	AttestationsBuffer [AttestationsMemory]AttestationSummary
	LatestVotesBuffer  [LatestVotesMemory]VoteSummary
}

type MemoryUpdater struct {
	lastMemoryDump       MemoryState
	currentMemory        Memory
	blocks               map[Root]BlockPtr
	votes                map[ValidatorIndex]LatestVotesPtr
	epochCommitteesCache map[Root][SLOTS_PER_EPOCH][MAX_COMMITTEES_PER_SLOT][]ValidatorIndex
	finalizedState       *phase0.BeaconState
	headState            *phase0.BeaconState
}

func (m *MemoryUpdater) PruneBlocks() {
	pruneBlockPtr := m.currentMemory.BlocksNextPtr
	if pruneBlockPtr < BlocksMemory {
		// nothing to prune
		return
	}
	pruneBlockPtr -= BlocksMemory
	for r, blockPtr := range m.blocks {
		if blockPtr < pruneBlockPtr {
			delete(m.blocks, r)
			// also prune epoch-committees corresponding to these blocks
			// if accidentally pruning more recent data (end of epoch still within view), then we can always refetch this data.
			delete(m.epochCommitteesCache, r)
		}
	}
}

func (m *MemoryUpdater) PruneVotes() {
	pruneLatestVotePtr := m.currentMemory.LatestVotesNextPtr
	if pruneLatestVotePtr < LatestVotesMemory {
		// nothing to prune
		return
	}
	pruneLatestVotePtr -= LatestVotesMemory
	for vi, votePtr := range m.votes {
		if votePtr < pruneLatestVotePtr {
			delete(m.votes, vi)
		}
	}
}

type MemoryDiff struct {
	Previous     MemoryState
	Current      MemoryState
	Head         []BlockPtr
	Finalized    []BlockPtr
	Blocks       []BlockSummary
	Attestations []AttestationSummary
	LatestVotes  []VoteSummary
}

func (m *MemoryUpdater) BuildDiff() *MemoryDiff {
	pre := m.lastMemoryDump
	now := m.currentMemory.MemoryState
	out := &MemoryDiff{
		Previous:     pre,
		Current:      now,
		Head:         make([]BlockPtr, 0, now.HeadNextPtr-pre.HeadNextPtr),
		Finalized:    make([]BlockPtr, 0, now.FinalizedNextPtr-pre.FinalizedNextPtr),
		Blocks:       make([]BlockSummary, 0, now.BlocksNextPtr-pre.BlocksNextPtr),
		Attestations: make([]AttestationSummary, 0, now.AttestationsNextPtr-pre.AttestationsNextPtr),
		LatestVotes:  make([]VoteSummary, 0, now.LatestVotesNextPtr-pre.LatestVotesNextPtr),
	}
	// No generics, and easier hardcoded than making MemoryDiff more abstract.
	// for each buffer: either the diff wraps around, or not
	{
		a := pre.HeadNextPtr % HeadsMemory
		b := now.HeadNextPtr % HeadsMemory
		if b < a {
			out.Head = append(out.Head, m.currentMemory.HeadBuffer[a:0]...)
			out.Head = append(out.Head, m.currentMemory.HeadBuffer[0:b]...)
		} else {
			out.Head = append(out.Head, m.currentMemory.HeadBuffer[a:b]...)
		}
	}
	{
		a := pre.FinalizedNextPtr % FinalizedMemory
		b := now.FinalizedNextPtr % FinalizedMemory
		if b < a {
			out.Finalized = append(out.Finalized, m.currentMemory.FinalizedBuffer[a:0]...)
			out.Finalized = append(out.Finalized, m.currentMemory.FinalizedBuffer[0:b]...)
		} else {
			out.Finalized = append(out.Finalized, m.currentMemory.FinalizedBuffer[a:b]...)
		}
	}
	{
		a := pre.BlocksNextPtr % BlocksMemory
		b := now.BlocksNextPtr % BlocksMemory
		if b < a {
			out.Blocks = append(out.Blocks, m.currentMemory.BlocksBuffer[a:0]...)
			out.Blocks = append(out.Blocks, m.currentMemory.BlocksBuffer[0:b]...)
		} else {
			out.Blocks = append(out.Blocks, m.currentMemory.BlocksBuffer[a:b]...)
		}
	}
	{
		a := pre.AttestationsNextPtr % AttestationsMemory
		b := now.AttestationsNextPtr % AttestationsMemory
		if b < a {
			out.Attestations = append(out.Attestations, m.currentMemory.AttestationsBuffer[a:0]...)
			out.Attestations = append(out.Attestations, m.currentMemory.AttestationsBuffer[0:b]...)
		} else {
			out.Attestations = append(out.Attestations, m.currentMemory.AttestationsBuffer[a:b]...)
		}
	}
	{
		a := pre.LatestVotesNextPtr % LatestVotesMemory
		b := now.LatestVotesNextPtr % LatestVotesMemory
		if b < a {
			out.LatestVotes = append(out.LatestVotes, m.currentMemory.LatestVotesBuffer[a:0]...)
			out.LatestVotes = append(out.LatestVotes, m.currentMemory.LatestVotesBuffer[0:b]...)
		} else {
			out.LatestVotes = append(out.LatestVotes, m.currentMemory.LatestVotesBuffer[a:b]...)
		}
	}
	return out
}

func (m *MemoryUpdater) OnEvent(ev *events.Event) {
	switch data := ev.Data.(type) {
	case *events.BeaconBlockImported:
		m.OnImportBlock(data)
	case *events.BeaconBlockRejected:
		m.OnRejectBlock(data)
	case *events.BeaconAttestationImported:
		m.OnImportAttestation(data)
	case *events.BeaconAttestationRejected:
		m.OnRejectAttestation(data)
	case *events.BeaconHeadChanged:
		m.OnHeadChange(data)
	case *events.BeaconFinalization:
		m.OnFinalize(data)
	}
}

func (m *MemoryUpdater) OnBlockIdentity(root Root, slot Slot, parent Root) BlockPtr {
	if i, ok := m.blocks[root]; ok {
		// backfill summary data
		if summary := &m.currentMemory.BlocksBuffer[i%BlocksMemory]; summary.Slot == 0 {
			summary.Parent = m.blocks[parent]
			summary.Slot = slot
		}
		return i
	} else {
		i = m.currentMemory.BlocksNextPtr
		m.blocks[root] = i
		m.currentMemory.BlocksBuffer[i%BlocksMemory] = BlockSummary{
			Slot:   slot,
			HTR:    root,
			Parent: m.blocks[parent], // may still be 0 if parent is unknown
		}
		m.currentMemory.BlocksNextPtr = i + 1
		return i
	}
}

func (m *MemoryUpdater) GetAttestation(attPtr AttestationPtr) *AttestationSummary {
	if attPtr+AttestationsMemory <= m.currentMemory.AttestationsNextPtr {
		// previous attestation is so outdated that it's not in memory anymore
		return nil
	}
	return &m.currentMemory.AttestationsBuffer[attPtr&AttestationsMemory]
}

func (m *MemoryUpdater) IsOutdatedVote(index ValidatorIndex, attPtr AttestationPtr) bool {
	prevPtr, ok := m.votes[index]
	if !ok {
		// no previous vote
		return true
	}
	if prevPtr+LatestVotesMemory <= m.currentMemory.LatestVotesNextPtr {
		// previous vote is so outdated that it's not in memory anymore
		return true
	}
	prevVote := m.currentMemory.LatestVotesBuffer[prevPtr%LatestVotesMemory]
	prevAtt := m.GetAttestation(prevVote.AttestationPtr)
	if prevAtt == nil {
		return true
	}
	currAtt := m.GetAttestation(attPtr)
	if currAtt == nil {
		return false
	}
	return prevAtt.Slot < currAtt.Slot
}

func (m *MemoryUpdater) OnVoteIdentity(index ValidatorIndex, attPtr AttestationPtr) LatestVotesPtr {
	if !m.IsOutdatedVote(index, attPtr) {
		return m.votes[index]
	} else {
		i := m.currentMemory.LatestVotesNextPtr
		m.votes[index] = i
		m.currentMemory.LatestVotesBuffer[i%LatestVotesMemory] = VoteSummary{
			ValidatorIndex: index,
			AttestationPtr: attPtr,
		}
		m.currentMemory.LatestVotesNextPtr = i + 1
		return i
	}
}

func (m *MemoryUpdater) OnImportBlock(data *events.BeaconBlockImported) {
	m.OnBlockIdentity(data.BlockRoot, data.Block.Slot, data.Block.ParentRoot)
	// TODO store block in leveldb?
}

func (m *MemoryUpdater) OnRejectBlock(data *events.BeaconBlockRejected) {
	// TODO handle rejected blocks?
}

func (m *MemoryUpdater) GetState(block Root) (*phase0.BeaconState, error) {
	// TODO http request to fetch SSZ state, deserialize it
	return nil, nil
}

// Used to get an earlier block, but after a given slot, as anchor point to get committee data with
func (m *MemoryUpdater) GetAncestorAtOrAfter(block Root, slot Slot) (Root, error) {
	prevRoot := block
	i := m.blocks[block]
	for {
		if i+BlocksMemory >= m.currentMemory.BlocksNextPtr {
			return Root{}, errors.New("ancestor too old, cannot find it in buffer")
		}
		b := &m.currentMemory.BlocksBuffer[i%BlocksMemory]
		if b.Slot == slot {
			return b.HTR, nil
		}
		if b.Slot < slot {
			return prevRoot, nil
		}
		i = b.Parent
		prevRoot = b.HTR
	}
}

// Subset of zrnt features to compute committee data for a whole epoch with
type CommitteeCompute struct {
	*phase0.BeaconState
	shuffling.ShufflingFeature
	seeding.SeedFeature
}

func NewCommitteeCompute(state *phase0.BeaconState) *CommitteeCompute {
	cc := new(CommitteeCompute)
	cc.BeaconState = state
	cc.ShufflingFeature.Meta = cc
	cc.SeedFeature.Meta = cc
	return cc
}

func (m *MemoryUpdater) GetCommittee(block Root, slot Slot, commIndex CommitteeIndex) ([]ValidatorIndex, error) {
	// try to get the first block that has access to this same committee data
	committeeAnchorBlock, err := m.GetAncestorAtOrAfter(block, slot.ToEpoch().Previous().GetStartSlot())
	if err != nil {
		committeeAnchorBlock = block
	}
	// check if we cached it
	if cached, ok := m.epochCommitteesCache[committeeAnchorBlock]; ok {
		return cached[slot-slot.ToEpoch().GetStartSlot()][commIndex], nil
	}
	// if not in the cache, then get the corresponding state, and fetch the data
	state, err := m.GetState(committeeAnchorBlock)
	if err != nil {
		return nil, fmt.Errorf("cannot get state to compute committees from, err: %v", err)
	}
	// compute the committees for the whole epoch
	committeeCompute := NewCommitteeCompute(state)
	shufEpoch := committeeCompute.LoadShufflingEpoch(slot.ToEpoch())
	m.epochCommitteesCache[committeeAnchorBlock] = shufEpoch.Committees
	return shufEpoch.Committees[slot-slot.ToEpoch().GetStartSlot()][commIndex], nil
}

func (m *MemoryUpdater) OnImportAttestation(data *events.BeaconAttestationImported) {
	s := m.OnBlockIdentity(data.Attestation.Data.Source.Root, data.Attestation.Data.Source.Epoch.GetStartSlot(), Root{})
	t := m.OnBlockIdentity(data.Attestation.Data.Target.Root, data.Attestation.Data.Target.Epoch.GetStartSlot(), Root{})
	h := m.OnBlockIdentity(data.Attestation.Data.BeaconBlockRoot, data.Attestation.Data.Slot, Root{})
	attPtr := m.currentMemory.AttestationsNextPtr
	m.currentMemory.AttestationsBuffer[attPtr%AttestationsMemory] = AttestationSummary{
		Slot:      data.Attestation.Data.Slot,
		CommIndex: data.Attestation.Data.Index,
		Head:      h,
		Target:    t,
		Source:    s,
	}
	m.currentMemory.AttestationsNextPtr++
	committee, err := m.GetCommittee(
		data.Attestation.Data.BeaconBlockRoot,
		data.Attestation.Data.Slot,
		data.Attestation.Data.Index)
	if err != nil {
		return
	}
	indexedAtt, err := data.Attestation.ConvertToIndexed(committee)
	if err != nil {
		return
	}
	for _, vi := range indexedAtt.AttestingIndices {
		m.OnVoteIdentity(vi, attPtr)
	}
}

func (m *MemoryUpdater) OnRejectAttestation(data *events.BeaconAttestationRejected) {
	//i := m.OnBlockIdentity(data.Attestation.Data.BeaconBlockRoot, Root{})
	// TODO anything on reject?
}

func (m *MemoryUpdater) OnHeadChange(data *events.BeaconHeadChanged) {
	i := m.OnBlockIdentity(data.CurrentHeadBeaconBlockRoot, 0, Root{})
	m.currentMemory.HeadBuffer[m.currentMemory.HeadNextPtr%HeadsMemory] = i
	m.currentMemory.HeadNextPtr = i

	// TODO get new head state, diff with previous state, and buffer balance changes
}

func (m *MemoryUpdater) OnFinalize(data *events.BeaconFinalization) {
	i := m.OnBlockIdentity(data.Root, 0, Root{})
	m.currentMemory.FinalizedBuffer[m.currentMemory.FinalizedNextPtr%FinalizedMemory] = i
	m.currentMemory.FinalizedNextPtr = i

	// TODO get new finalized state
}
