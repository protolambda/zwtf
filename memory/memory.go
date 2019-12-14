package memory

import (
	"errors"
	"fmt"
	"github.com/protolambda/zrnt/eth2/beacon/eth1"
	"github.com/protolambda/zrnt/eth2/beacon/seeding"
	"github.com/protolambda/zrnt/eth2/beacon/shuffling"
	. "github.com/protolambda/zrnt/eth2/core"
	"github.com/protolambda/zrnt/eth2/phase0"
	"log"
	"sync"
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

type FFG struct {
	Source Gwei
	Target Gwei
	Head   Gwei
}

type ValidatorCounts struct {
	Total        uint32
	Active       uint32
	Slashed      uint32
	Eligible     uint32
	NonEligible  uint32
	Exiting      uint32
	Withdrawable uint32
}

type HeadSummary struct {
	HeadBlock       BlockPtr
	Slot            Slot
	ProposerIndex   ValidatorIndex
	ValidatorCounts ValidatorCounts
	TotalStaked     Gwei
	AvgBalance      Gwei
	DepositIndex    DepositIndex
	Eth1Data        eth1.Eth1Data
	PreviousFFG     FFG
	CurrentFFG      FFG
}

func (h *HeadSummary) Display() string {
	return fmt.Sprintf(`
----
slot: %7d   block ptr: %d
proposer index: %d
validator counts:
			  active:  %d
			 slashed:  %d
			eligible:  %d
		 noneligible:  %d
			 exiting:  %d
		withdrawable:  %d
		       total:  %d
total staked: %10d Gwei
avg balance:  %10d Gwei
deposit index: %d
eth1 data:
   eth1 block hash: %x
     deposit count: %d
      deposit root: %x
previous epoch ffg:
   source: %10d Gwei
   target: %10d Gwei
   head:   %10d Gwei
current epoch ffg:
   source: %10d Gwei
   target: %10d Gwei
   head:   %10d Gwei
----
`, h.Slot, h.HeadBlock,
		h.ProposerIndex,
		h.ValidatorCounts.Active, h.ValidatorCounts.Slashed, h.ValidatorCounts.Eligible,
		h.ValidatorCounts.NonEligible, h.ValidatorCounts.Exiting, h.ValidatorCounts.Withdrawable,
		h.ValidatorCounts.Total,
		h.TotalStaked, h.AvgBalance,
		h.DepositIndex,
		h.Eth1Data.BlockHash, h.Eth1Data.DepositCount, h.Eth1Data.DepositRoot,
		h.PreviousFFG.Source, h.PreviousFFG.Target, h.PreviousFFG.Head,
		h.CurrentFFG.Source, h.CurrentFFG.Target, h.CurrentFFG.Head)
}

func HeadSummaryFromState(state *phase0.BeaconState, blockPtr BlockPtr) *HeadSummary {
	fstate := phase0.NewFullFeaturedState(state)
	fstate.LoadPrecomputedData()
	currentEpoch := state.CurrentEpoch()
	summary := new(HeadSummary)
	summary.Slot = state.Slot
	summary.HeadBlock = blockPtr
	for i, v := range state.Validators {
		if v.IsActive(currentEpoch) {
			summary.ValidatorCounts.Active += 1
			summary.TotalStaked += v.EffectiveBalance
		} else if v.Slashed {
			summary.ValidatorCounts.Slashed += 1
		} else if v.ActivationEligibilityEpoch < currentEpoch {
			summary.ValidatorCounts.NonEligible += 1
		} else if v.WithdrawableEpoch > currentEpoch {
			summary.ValidatorCounts.Exiting += 1
		} else {
			summary.ValidatorCounts.Withdrawable += 1
		}
		summary.AvgBalance += state.GetBalance(ValidatorIndex(i))
	}
	summary.ValidatorCounts.Total = uint32(len(state.Validators))
	summary.AvgBalance /= Gwei(len(state.Validators))
	summary.DepositIndex = state.DepositIndex
	summary.Eth1Data = state.Eth1Data
	summary.ProposerIndex = fstate.GetBeaconProposerIndex(state.Slot)
	attesterStatuses := fstate.GetAttesterStatuses()
	summary.PreviousFFG.Source = fstate.GetAttestersStake(attesterStatuses, PrevSourceAttester|UnslashedAttester)
	summary.PreviousFFG.Target = fstate.GetAttestersStake(attesterStatuses, PrevTargetAttester|UnslashedAttester)
	summary.PreviousFFG.Head = fstate.GetAttestersStake(attesterStatuses, PrevHeadAttester|UnslashedAttester)
	summary.CurrentFFG.Source = fstate.GetAttestersStake(attesterStatuses, CurrSourceAttester|UnslashedAttester)
	summary.CurrentFFG.Target = fstate.GetAttestersStake(attesterStatuses, CurrTargetAttester|UnslashedAttester)
	summary.CurrentFFG.Head = fstate.GetAttestersStake(attesterStatuses, CurrHeadAttester|UnslashedAttester)
	return summary
}

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
	HeadBuffer         [HeadsMemory]*HeadSummary
	FinalizedBuffer    [FinalizedMemory]BlockPtr
	BlocksBuffer       [BlocksMemory]BlockSummary
	AttestationsBuffer [AttestationsMemory]AttestationSummary
	LatestVotesBuffer  [LatestVotesMemory]VoteSummary
}

type StateGetter func(blockRoot Root) (*phase0.BeaconState, error)

type MemoryManager struct {
	sync.Mutex
	lastMemoryDump       MemoryState
	currentMemory        Memory
	blocks               map[Root]BlockPtr
	votes                map[ValidatorIndex]LatestVotesPtr
	epochCommitteesCache map[Root][SLOTS_PER_EPOCH][MAX_COMMITTEES_PER_SLOT][]ValidatorIndex
	finalizedState       *phase0.BeaconState
	headState            *phase0.BeaconState
	getState             StateGetter
}

func NewMemoryManager(getState StateGetter) *MemoryManager {
	return &MemoryManager{
		lastMemoryDump:       MemoryState{},
		currentMemory:        Memory{},
		blocks:               make(map[Root]BlockPtr),
		votes:                make(map[ValidatorIndex]LatestVotesPtr),
		epochCommitteesCache: make(map[Root][SLOTS_PER_EPOCH][MAX_COMMITTEES_PER_SLOT][]ValidatorIndex),
		finalizedState:       nil,
		headState:            nil,
		getState:             getState,
	}
}

func (m *MemoryManager) PruneBlocks() {
	m.Lock()
	defer m.Unlock()
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

func (m *MemoryManager) PruneVotes() {
	m.Lock()
	defer m.Unlock()
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
	Head         []*HeadSummary
	Finalized    []BlockPtr
	Blocks       []BlockSummary
	Attestations []AttestationSummary
	LatestVotes  []VoteSummary
}

func (m *MemoryManager) BuildDiff() *MemoryDiff {
	m.Lock()
	defer m.Unlock()
	pre := m.lastMemoryDump
	now := m.currentMemory.MemoryState
	out := &MemoryDiff{
		Previous:     pre,
		Current:      now,
		Head:         make([]*HeadSummary, 0, now.HeadNextPtr-pre.HeadNextPtr),
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
	m.lastMemoryDump = out.Current
	return out
}

func (m *MemoryManager) OnEvent(ev *events.Event) {
	m.Lock()
	defer m.Unlock()
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

func (m *MemoryManager) OnBlockIdentity(root Root, slot Slot, parent Root) BlockPtr {
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
		m.currentMemory.BlocksNextPtr += 1
		return i
	}
}

func (m *MemoryManager) GetAttestation(attPtr AttestationPtr) *AttestationSummary {
	if attPtr+AttestationsMemory <= m.currentMemory.AttestationsNextPtr {
		// previous attestation is so outdated that it's not in memory anymore
		return nil
	}
	return &m.currentMemory.AttestationsBuffer[attPtr&AttestationsMemory]
}

func (m *MemoryManager) IsOutdatedVote(index ValidatorIndex, attPtr AttestationPtr) bool {
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

func (m *MemoryManager) OnVoteIdentity(index ValidatorIndex, attPtr AttestationPtr) LatestVotesPtr {
	if !m.IsOutdatedVote(index, attPtr) {
		return m.votes[index]
	} else {
		i := m.currentMemory.LatestVotesNextPtr
		m.votes[index] = i
		m.currentMemory.LatestVotesBuffer[i%LatestVotesMemory] = VoteSummary{
			ValidatorIndex: index,
			AttestationPtr: attPtr,
		}
		m.currentMemory.LatestVotesNextPtr += 1
		return i
	}
}

func (m *MemoryManager) OnImportBlock(data *events.BeaconBlockImported) {
	log.Printf("OnImportBlock! %x %d", data.BlockRoot, data.Block.Slot)
	m.OnBlockIdentity(data.BlockRoot, data.Block.Slot, data.Block.ParentRoot)
	// TODO store block in leveldb?
}

func (m *MemoryManager) OnRejectBlock(data *events.BeaconBlockRejected) {
	log.Printf("OnRejectBlock: %s", data.Reason)
	// TODO handle rejected blocks?
}

// Used to get an earlier block, but after a given slot, as anchor point to get committee data with
func (m *MemoryManager) GetAncestorAtOrAfter(block Root, slot Slot) (Root, error) {
	prevRoot := block
	i := m.blocks[block]
	if i == 0 {
		return prevRoot, nil
	}
	for {
		if i+BlocksMemory <= m.currentMemory.BlocksNextPtr {
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

func (m *MemoryManager) GetCommittee(block Root, slot Slot, commIndex CommitteeIndex) ([]ValidatorIndex, error) {
	log.Printf("Getting committee data for attestation! %x %d %d", block, slot, commIndex)
	// try to get the first block that has access to this same committee data
	committeeAnchorBlock, err := m.GetAncestorAtOrAfter(block, slot.ToEpoch().Previous().GetStartSlot())
	if err != nil {
		log.Printf("warning: ancestor for attestation is not part of buffered data, fetching state for block itself. Err: %v", err)
		committeeAnchorBlock = block
	}
	// check if we cached it
	if cached, ok := m.epochCommitteesCache[committeeAnchorBlock]; ok {
		return cached[slot-slot.ToEpoch().GetStartSlot()][commIndex], nil
	}
	// if not in the cache, then get the corresponding state, and fetch the data
	log.Printf("Fetching new state of block %x to compute committee data for attestation! %x %d %d", committeeAnchorBlock, block, slot, commIndex)
	state, err := m.getState(committeeAnchorBlock)
	if err != nil {
		return nil, fmt.Errorf("cannot get state to compute committees from, err: %v", err)
	}
	// compute the committees for the whole epoch
	committeeCompute := NewCommitteeCompute(state)
	shufEpoch := committeeCompute.LoadShufflingEpoch(slot.ToEpoch())
	m.epochCommitteesCache[committeeAnchorBlock] = shufEpoch.Committees
	log.Printf("Fetched state of block %x and computed and cached committee data for its future epoch %d", committeeAnchorBlock, slot.ToEpoch())
	return shufEpoch.Committees[slot-slot.ToEpoch().GetStartSlot()][commIndex], nil
}

func (m *MemoryManager) OnImportAttestation(data *events.BeaconAttestationImported) {
	log.Printf("OnImportAttestation! %x %d %d", data.Attestation.Data.BeaconBlockRoot, data.Attestation.Data.Slot, data.Attestation.Data.Index)
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
	m.currentMemory.AttestationsNextPtr += 1
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
	log.Printf("Successfully processed attestation! %x %d %d", data.Attestation.Data.BeaconBlockRoot, data.Attestation.Data.Slot, data.Attestation.Data.Index)
}

func (m *MemoryManager) OnRejectAttestation(data *events.BeaconAttestationRejected) {
	log.Printf("OnRejectAttestation: %s", data.Reason)
	//i := m.OnBlockIdentity(data.Attestation.Data.BeaconBlockRoot, Root{})
	// TODO anything on reject?
}

func (m *MemoryManager) OnHeadChange(data *events.BeaconHeadChanged) {
	log.Printf("OnHeadChange! %x %x %v", data.CurrentHeadBeaconBlockRoot, data.PreviousHeadBeaconBlockRoot, data.Reorg)
	state, err := m.getState(data.CurrentHeadBeaconBlockRoot)
	if err != nil {
		log.Printf("warning: cannot fetch head state for block root %x: %v", data.CurrentHeadBeaconBlockRoot, err)
		return
	}
	log.Printf("retrieved new head state for slot: %d", state.Slot)

	i := m.OnBlockIdentity(data.CurrentHeadBeaconBlockRoot, 0, Root{})
	headSummary := HeadSummaryFromState(state, i)
	m.currentMemory.HeadBuffer[m.currentMemory.HeadNextPtr%HeadsMemory] = headSummary
	m.currentMemory.HeadNextPtr += 1
	log.Println("new head summary:")
	log.Println(headSummary.Display())

	m.headState = state
}

func (m *MemoryManager) OnFinalize(data *events.BeaconFinalization) {
	log.Printf("OnFinalize! %x %d", data.Root, data.Epoch)
	i := m.OnBlockIdentity(data.Root, 0, Root{})
	m.currentMemory.FinalizedBuffer[m.currentMemory.FinalizedNextPtr%FinalizedMemory] = i
	m.currentMemory.FinalizedNextPtr += 1

	state, err := m.getState(data.Root)
	if err != nil {
		log.Printf("warning: cannot fetch finalized state for block root %x: %v", data.Root, err)
		return
	}
	log.Printf("retrieved new finalized state for slot: %d", state.Slot)
	m.finalizedState = state
}
