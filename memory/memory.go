package memory

import (
	"bytes"
	"encoding/hex"
	"errors"
	"fmt"
	"github.com/protolambda/zrnt/eth2/beacon/seeding"
	"github.com/protolambda/zrnt/eth2/beacon/shuffling"
	"github.com/protolambda/zrnt/eth2/core"
	"github.com/protolambda/zrnt/eth2/phase0"
	"github.com/protolambda/zwtf/events"
	"log"
	"sync"
)

type Gwei = core.Gwei
type Slot = core.Slot
type ValidatorIndex = core.ValidatorIndex
type DepositIndex = core.DepositIndex
type CommitteeIndex = core.CommitteeIndex

type Root core.Root

func (r *Root) MarshalJSON() ([]byte, error) {
	out := make([]byte, 64+2, 64+2)
	out[0] = '"'
	hex.Encode(out[1:65], r[:])
	out[65] = '"'
	return out, nil
}

const HeadsMemory = 1000
const FinalizedMemory = 1000
const BlocksMemory = 1000
const AttestationsMemory = 1000
const LatestVotesMemory = 10000

type BlockPtr uint32
const EmptyBlockMarker = ^BlockPtr(0)
type AttestationPtr uint32
type LatestVotesPtr uint32

type FFG struct {
	Source Gwei `json:"source"`
	Target Gwei `json:"target"`
	Head   Gwei `json:"head"`
}

type ValidatorCounts struct {
	Total        uint32 `json:"total"`
	Active       uint32 `json:"active"`
	Slashed      uint32 `json:"slashed"`
	Eligible     uint32 `json:"eligible"`
	NonEligible  uint32 `json:"nonEligible"`
	Exiting      uint32 `json:"exiting"`
	Withdrawable uint32 `json:"withdrawable"`
}

type Eth1Data struct {
	DepositRoot  Root `json:"depositRoot"`
	DepositCount DepositIndex `json:"depositCount"`
	BlockHash    Root `json:"blockHash"`
}

type HeadSummary struct {
	HeadBlock       BlockPtr `json:"headBlock"`
	Slot            Slot `json:"slot"`
	ProposerIndex   ValidatorIndex `json:"proposerIndex"`
	ValidatorCounts ValidatorCounts `json:"validatorCounts"`
	TotalStaked     Gwei `json:"totalStaked"`
	AvgBalance      Gwei `json:"avgBalance"`
	DepositIndex    DepositIndex `json:"depositIndex"`
	Eth1Data        Eth1Data `json:"eth1Data"`
	PreviousFFG     FFG `json:"previousFFG"`
	CurrentFFG      FFG `json:"currentFFG"`
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
	summary.Eth1Data = Eth1Data{
		DepositRoot:  Root(state.Eth1Data.DepositRoot),
		DepositCount: state.Eth1Data.DepositCount,
		BlockHash:    Root(state.Eth1Data.BlockHash),
	}
	summary.ProposerIndex = fstate.GetBeaconProposerIndex(state.Slot)
	attesterStatuses := fstate.GetAttesterStatuses()
	summary.PreviousFFG.Source = fstate.GetAttestersStake(attesterStatuses, core.PrevSourceAttester|core.UnslashedAttester)
	summary.PreviousFFG.Target = fstate.GetAttestersStake(attesterStatuses, core.PrevTargetAttester|core.UnslashedAttester)
	summary.PreviousFFG.Head = fstate.GetAttestersStake(attesterStatuses,   core.PrevHeadAttester  |core.UnslashedAttester)
	summary.CurrentFFG.Source = fstate.GetAttestersStake(attesterStatuses,  core.CurrSourceAttester|core.UnslashedAttester)
	summary.CurrentFFG.Target = fstate.GetAttestersStake(attesterStatuses,  core.CurrTargetAttester|core.UnslashedAttester)
	summary.CurrentFFG.Head = fstate.GetAttestersStake(attesterStatuses,    core.CurrHeadAttester  |core.UnslashedAttester)
	return summary
}

type BlockSummary struct {
	SelfPtr BlockPtr `json:"selfPtr"`
	HTR     Root `json:"htr"`
	Slot    Slot `json:"slot"`
	Parent  BlockPtr `json:"parent"`
}

type AttestationSummary struct {
	SelfPtr   AttestationPtr `json:"selfPtr"`
	Slot      Slot `json:"slot"`
	CommIndex CommitteeIndex `json:"commIndex"`
	Head      BlockPtr `json:"head"`
	Target    BlockPtr `json:"target"`
	Source    BlockPtr `json:"source"`
}

type VoteSummary struct {
	ValidatorIndex ValidatorIndex `json:"validatorIndex"`
	AttestationPtr AttestationPtr `json:"attestationPtr"`
}

type MemoryState struct {
	// ptrs rotate around buffer
	HeadNextPtr         BlockPtr       `json:"head"` // index modulo HeadsMemory
	FinalizedNextPtr    BlockPtr       `json:"finalized"` // index modulo FinalizedMemory
	BlocksNextPtr       BlockPtr       `json:"blocks"` // index modulo BlocksMemory
	AttestationsNextPtr AttestationPtr `json:"attestations"` // index modulo AttestationsMemory
	LatestVotesNextPtr  LatestVotesPtr `json:"latestVotes"` // index modulo LatestVotesMemory
}

type Memory struct {
	MemoryState
	HeadBuffer         [HeadsMemory]*HeadSummary
	FinalizedBuffer    [FinalizedMemory]BlockPtr
	BlocksBuffer       [BlocksMemory]BlockSummary
	AttestationsBuffer [AttestationsMemory]AttestationSummary
	LatestVotesBuffer  [LatestVotesMemory]VoteSummary
}

type StateGetter func(blockRoot core.Root) (*phase0.BeaconState, error)

type MemoryManager struct {
	sync.Mutex
	lastMemoryDump       MemoryState
	nextMemoryDumpIndex  uint64
	currentMemory        Memory
	blocks               map[Root]BlockPtr
	votes                map[ValidatorIndex]LatestVotesPtr
	epochCommitteesCache map[Root][core.SLOTS_PER_EPOCH][core.MAX_COMMITTEES_PER_SLOT][]ValidatorIndex
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
		epochCommitteesCache: make(map[Root][core.SLOTS_PER_EPOCH][core.MAX_COMMITTEES_PER_SLOT][]ValidatorIndex),
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
	DiffIndex    uint64 `json:"diffIndex"`
	Previous     MemoryState `json:"previous"`
	Head         []*HeadSummary `json:"head"`
	Finalized    []BlockPtr `json:"finalized"`
	Blocks       []BlockSummary `json:"blocks"`
	Attestations []AttestationSummary `json:"attestations"`
	LatestVotes  []VoteSummary `json:"latestVotes"`
}

func (d *MemoryDiff) Display() string {
	var buf bytes.Buffer
	buf.WriteString(fmt.Sprintf("diff index: %d\n", d.DiffIndex))
	buf.WriteString(fmt.Sprintf("previous state: %v\n", d.Previous))
	for _, h := range d.Head {
		buf.WriteString(h.Display())
	}
	buf.WriteString(fmt.Sprintf("finalized blocks (ptrs): %v\n", d.Finalized))
	buf.WriteString("--- new blocks ---\n")
	for _, b := range d.Blocks {
		buf.WriteString(fmt.Sprintf("block: self: %d, root: %x slot: %d, parent: %d\n",
			b.SelfPtr, b.HTR, b.Slot, b.Parent))
	}
	buf.WriteString("--- new attesations ---\n")
	for _, a := range d.Attestations {
		buf.WriteString(fmt.Sprintf("attestation: head: %d, target: %d source: %d, slot: %d, comm index: %d\n",
			a.Head, a.Target, a.Source, a.Slot, a.CommIndex))
	}
	buf.WriteString("--- new votes ---\n")
	buf.WriteString(fmt.Sprintf("%v\n", d.LatestVotes))
	return buf.String()
}

func (m *MemoryManager) BuildDiff() *MemoryDiff {
	m.Lock()
	defer m.Unlock()
	pre := m.lastMemoryDump
	now := m.currentMemory.MemoryState
	out := &MemoryDiff{
		Previous:     pre,
		DiffIndex:    m.nextMemoryDumpIndex,
		Head:         make([]*HeadSummary, 0, now.HeadNextPtr-pre.HeadNextPtr),
		Finalized:    make([]BlockPtr, 0, now.FinalizedNextPtr-pre.FinalizedNextPtr),
		Blocks:       make([]BlockSummary, 0, now.BlocksNextPtr-pre.BlocksNextPtr),
		Attestations: make([]AttestationSummary, 0, now.AttestationsNextPtr-pre.AttestationsNextPtr),
		LatestVotes:  make([]VoteSummary, 0, now.LatestVotesNextPtr-pre.LatestVotesNextPtr),
	}
	m.nextMemoryDumpIndex += 1
	// No generics, and easier hardcoded than making MemoryDiff more abstract.
	// for each buffer: either the diff wraps around, or not
	{
		a := pre.HeadNextPtr % HeadsMemory
		b := now.HeadNextPtr % HeadsMemory
		if b < a {
			out.Head = append(out.Head, m.currentMemory.HeadBuffer[a:]...)
			out.Head = append(out.Head, m.currentMemory.HeadBuffer[:b]...)
		} else {
			out.Head = append(out.Head, m.currentMemory.HeadBuffer[a:b]...)
		}
	}
	{
		a := pre.FinalizedNextPtr % FinalizedMemory
		b := now.FinalizedNextPtr % FinalizedMemory
		if b < a {
			out.Finalized = append(out.Finalized, m.currentMemory.FinalizedBuffer[a:]...)
			out.Finalized = append(out.Finalized, m.currentMemory.FinalizedBuffer[:b]...)
		} else {
			out.Finalized = append(out.Finalized, m.currentMemory.FinalizedBuffer[a:b]...)
		}
	}
	{
		a := pre.BlocksNextPtr % BlocksMemory
		b := now.BlocksNextPtr % BlocksMemory
		if b < a {
			out.Blocks = append(out.Blocks, m.currentMemory.BlocksBuffer[a:]...)
			out.Blocks = append(out.Blocks, m.currentMemory.BlocksBuffer[:b]...)
		} else {
			out.Blocks = append(out.Blocks, m.currentMemory.BlocksBuffer[a:b]...)
		}
	}
	{
		a := pre.AttestationsNextPtr % AttestationsMemory
		b := now.AttestationsNextPtr % AttestationsMemory
		if b < a {
			out.Attestations = append(out.Attestations, m.currentMemory.AttestationsBuffer[a:]...)
			out.Attestations = append(out.Attestations, m.currentMemory.AttestationsBuffer[:b]...)
		} else {
			out.Attestations = append(out.Attestations, m.currentMemory.AttestationsBuffer[a:b]...)
		}
	}
	{
		a := pre.LatestVotesNextPtr % LatestVotesMemory
		b := now.LatestVotesNextPtr % LatestVotesMemory
		if b < a {
			out.LatestVotes = append(out.LatestVotes, m.currentMemory.LatestVotesBuffer[a:]...)
			out.LatestVotes = append(out.LatestVotes, m.currentMemory.LatestVotesBuffer[:b]...)
		} else {
			out.LatestVotes = append(out.LatestVotes, m.currentMemory.LatestVotesBuffer[a:b]...)
		}
	}
	m.lastMemoryDump = now
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
		parentBlockPtr, ok := m.blocks[parent]
		if !ok {
			parentBlockPtr = EmptyBlockMarker
		}
		m.currentMemory.BlocksBuffer[i%BlocksMemory] = BlockSummary{
			SelfPtr: i,
			Slot:    slot,
			HTR:     root,
			Parent:  parentBlockPtr,
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
	m.OnBlockIdentity(Root(data.BlockRoot), data.Block.Slot, Root(data.Block.ParentRoot))
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
	for {
		if i == EmptyBlockMarker {
			return prevRoot, errors.New("buffer not deep enough, cannot find ancestor block")
		}
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
		log.Println("committee cache hit")
		return cached[slot-slot.ToEpoch().GetStartSlot()][commIndex], nil
	}
	// if not in the cache, then get the corresponding state, and fetch the data
	log.Printf("Fetching new state of block %x to compute committee data for attestation! %x %d %d", committeeAnchorBlock, block, slot, commIndex)
	state, err := m.getState(core.Root(committeeAnchorBlock))
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
	s := m.OnBlockIdentity(Root(data.Attestation.Data.Source.Root), data.Attestation.Data.Source.Epoch.GetStartSlot(), Root{})
	t := m.OnBlockIdentity(Root(data.Attestation.Data.Target.Root), data.Attestation.Data.Target.Epoch.GetStartSlot(), Root{})
	h := m.OnBlockIdentity(Root(data.Attestation.Data.BeaconBlockRoot), data.Attestation.Data.Slot, Root{})
	attPtr := m.currentMemory.AttestationsNextPtr
	m.currentMemory.AttestationsBuffer[attPtr%AttestationsMemory] = AttestationSummary{
		SelfPtr:   attPtr,
		Slot:      data.Attestation.Data.Slot,
		CommIndex: data.Attestation.Data.Index,
		Head:      h,
		Target:    t,
		Source:    s,
	}
	m.currentMemory.AttestationsNextPtr += 1
	committee, err := m.GetCommittee(
		Root(data.Attestation.Data.BeaconBlockRoot),
		data.Attestation.Data.Slot,
		data.Attestation.Data.Index)
	if err != nil {
		log.Printf("Failed to process attestation! %x %d %d err: %v", data.Attestation.Data.BeaconBlockRoot, data.Attestation.Data.Slot, data.Attestation.Data.Index, err)
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

	i := m.OnBlockIdentity(Root(data.CurrentHeadBeaconBlockRoot), 0, Root{})
	headSummary := HeadSummaryFromState(state, i)
	m.currentMemory.HeadBuffer[m.currentMemory.HeadNextPtr%HeadsMemory] = headSummary
	m.currentMemory.HeadNextPtr += 1
	log.Println("new head summary:")
	log.Println(headSummary.Display())

	m.headState = state
}

func (m *MemoryManager) OnFinalize(data *events.BeaconFinalization) {
	log.Printf("OnFinalize! %x %d", data.Root, data.Epoch)
	i := m.OnBlockIdentity(Root(data.Root), 0, Root{})
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
