package memory

import (
	. "github.com/protolambda/zrnt/eth2/core"
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
	Slot   Slot
	Head   BlockPtr
	Target BlockPtr
	Source BlockPtr
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
	LastMemoryDump MemoryState
	CurrentMemory  Memory
	Blocks         map[Root]BlockPtr
	Votes          map[ValidatorIndex]LatestVotesPtr
}

func (m *MemoryUpdater) PruneBlocks() {
	pruneBlockPtr := m.CurrentMemory.BlocksNextPtr
	if pruneBlockPtr < BlocksMemory {
		// nothing to prune
		return
	}
	pruneBlockPtr -= BlocksMemory
	for r, blockPtr := range m.Blocks {
		if blockPtr < pruneBlockPtr {
			delete(m.Blocks, r)
		}
	}
}

func (m *MemoryUpdater) PruneVotes() {
	pruneLatestVotePtr := m.CurrentMemory.LatestVotesNextPtr
	if pruneLatestVotePtr < LatestVotesMemory {
		// nothing to prune
		return
	}
	pruneLatestVotePtr -= LatestVotesMemory
	for vi, votePtr := range m.Votes {
		if votePtr < pruneLatestVotePtr {
			delete(m.Votes, vi)
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
	pre := m.LastMemoryDump
	now := m.CurrentMemory.MemoryState
	out := &MemoryDiff{
		Previous: pre,
		Current: now,
		Head: make([]BlockPtr, 0, now.HeadNextPtr - pre.HeadNextPtr),
		Finalized: make([]BlockPtr, 0, now.FinalizedNextPtr - pre.FinalizedNextPtr),
		Blocks: make([]BlockSummary, 0, now.BlocksNextPtr - pre.BlocksNextPtr),
		Attestations: make([]AttestationSummary, 0, now.AttestationsNextPtr - pre.AttestationsNextPtr),
		LatestVotes: make([]VoteSummary, 0, now.LatestVotesNextPtr - pre.LatestVotesNextPtr),
	}
	// No generics, and easier hardcoded than making MemoryDiff more abstract.
	// for each buffer: either the diff wraps around, or not
	{
		a := pre.HeadNextPtr % HeadsMemory
		b := now.HeadNextPtr % HeadsMemory
		if b < a {
			out.Head = append(out.Head, m.CurrentMemory.HeadBuffer[a:0]...)
			out.Head = append(out.Head, m.CurrentMemory.HeadBuffer[0:b]...)
		} else {
			out.Head = append(out.Head, m.CurrentMemory.HeadBuffer[a:b]...)
		}
	}
	{
		a := pre.FinalizedNextPtr % FinalizedMemory
		b := now.FinalizedNextPtr % FinalizedMemory
		if b < a {
			out.Finalized = append(out.Finalized, m.CurrentMemory.FinalizedBuffer[a:0]...)
			out.Finalized = append(out.Finalized, m.CurrentMemory.FinalizedBuffer[0:b]...)
		} else {
			out.Finalized = append(out.Finalized, m.CurrentMemory.FinalizedBuffer[a:b]...)
		}
	}
	{
		a := pre.BlocksNextPtr % BlocksMemory
		b := now.BlocksNextPtr % BlocksMemory
		if b < a {
			out.Blocks = append(out.Blocks, m.CurrentMemory.BlocksBuffer[a:0]...)
			out.Blocks = append(out.Blocks, m.CurrentMemory.BlocksBuffer[0:b]...)
		} else {
			out.Blocks = append(out.Blocks, m.CurrentMemory.BlocksBuffer[a:b]...)
		}
	}
	{
		a := pre.AttestationsNextPtr % AttestationsMemory
		b := now.AttestationsNextPtr % AttestationsMemory
		if b < a {
			out.Attestations = append(out.Attestations, m.CurrentMemory.AttestationsBuffer[a:0]...)
			out.Attestations = append(out.Attestations, m.CurrentMemory.AttestationsBuffer[0:b]...)
		} else {
			out.Attestations = append(out.Attestations, m.CurrentMemory.AttestationsBuffer[a:b]...)
		}
	}
	{
		a := pre.LatestVotesNextPtr % LatestVotesMemory
		b := now.LatestVotesNextPtr % LatestVotesMemory
		if b < a {
			out.LatestVotes = append(out.LatestVotes, m.CurrentMemory.LatestVotesBuffer[a:0]...)
			out.LatestVotes = append(out.LatestVotes, m.CurrentMemory.LatestVotesBuffer[0:b]...)
		} else {
			out.LatestVotes = append(out.LatestVotes, m.CurrentMemory.LatestVotesBuffer[a:b]...)
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
	if i, ok := m.Blocks[root]; ok {
		// backfill summary data
		if summary := &m.CurrentMemory.BlocksBuffer[i%BlocksMemory]; summary.Slot == 0 {
			summary.Parent = m.Blocks[parent]
			summary.Slot = slot
		}
		return i
	} else {
		i = m.CurrentMemory.BlocksNextPtr
		m.Blocks[root] = i
		m.CurrentMemory.BlocksBuffer[i%BlocksMemory] = BlockSummary{
			Slot:   slot,
			HTR:    root,
			Parent: m.Blocks[parent], // may still be 0 if parent is unknown
		}
		m.CurrentMemory.BlocksNextPtr = i + 1
		return i
	}
}

func (m *MemoryUpdater) GetAttestation(attPtr AttestationPtr) *AttestationSummary {
	if attPtr+AttestationsMemory <= m.CurrentMemory.AttestationsNextPtr {
		// previous attestation is so outdated that it's not in memory anymore
		return nil
	}
	return &m.CurrentMemory.AttestationsBuffer[attPtr&AttestationsMemory]
}

func (m *MemoryUpdater) IsOutdatedVote(index ValidatorIndex, attPtr AttestationPtr) bool {
	prevPtr, ok := m.Votes[index]
	if !ok {
		// no previous vote
		return true
	}
	if prevPtr+LatestVotesMemory <= m.CurrentMemory.LatestVotesNextPtr {
		// previous vote is so outdated that it's not in memory anymore
		return true
	}
	prevVote := m.CurrentMemory.LatestVotesBuffer[prevPtr%LatestVotesMemory]
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
		return m.Votes[index]
	} else {
		i := m.CurrentMemory.LatestVotesNextPtr
		m.Votes[index] = i
		m.CurrentMemory.LatestVotesBuffer[i%LatestVotesMemory] = VoteSummary{
			ValidatorIndex: index,
			AttestationPtr: attPtr,
		}
		m.CurrentMemory.LatestVotesNextPtr = i + 1
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

//func (m *MemoryUpdater) GetAncestor(block Root, slot Slot) (Root, error) {
//	i := m.Blocks[block]
//	if i+BlocksMemory >= m.CurrentMemory.BlocksNextPtr {
//
//	}
//	b := &m.CurrentMemory.BlocksBuffer[i%BlocksMemory]
//}

func (m *MemoryUpdater) GetCommittee(block Root) ([]ValidatorIndex, error) {
	// TODO
	return nil, nil
}

func (m *MemoryUpdater) OnImportAttestation(data *events.BeaconAttestationImported) {
	s := m.OnBlockIdentity(data.Attestation.Data.Source.Root, data.Attestation.Data.Source.Epoch.GetStartSlot(), Root{})
	t := m.OnBlockIdentity(data.Attestation.Data.Target.Root, data.Attestation.Data.Target.Epoch.GetStartSlot(), Root{})
	h := m.OnBlockIdentity(data.Attestation.Data.BeaconBlockRoot, data.Attestation.Data.Slot, Root{})
	attPtr := m.CurrentMemory.AttestationsNextPtr
	m.CurrentMemory.AttestationsBuffer[attPtr%AttestationsMemory] = AttestationSummary{
		Slot:   data.Attestation.Data.Slot,
		Head:   h,
		Target: t,
		Source: s,
	}
	m.CurrentMemory.AttestationsNextPtr++
	committee, err := m.GetCommittee(data.Attestation.Data.BeaconBlockRoot)
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
	m.CurrentMemory.HeadBuffer[m.CurrentMemory.HeadNextPtr%HeadsMemory] = i
	m.CurrentMemory.HeadNextPtr = i
}

func (m *MemoryUpdater) OnFinalize(data *events.BeaconFinalization) {
	i := m.OnBlockIdentity(data.Root, 0, Root{})
	m.CurrentMemory.FinalizedBuffer[m.CurrentMemory.FinalizedNextPtr%FinalizedMemory] = i
	m.CurrentMemory.FinalizedNextPtr = i
}
