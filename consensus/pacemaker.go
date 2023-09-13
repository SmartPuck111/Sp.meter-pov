// Copyright (c) 2020 The Meter.io developers

// Distributed under the GNU Lesser General Public License v3.0 software license, see the accompanying
// file LICENSE or <https://www.gnu.org/licenses/lgpl-3.0.html>

package consensus

import (
	"fmt"
	"os"
	"time"

	"github.com/inconshreveable/log15"
	"github.com/meterio/meter-pov/block"
	"github.com/meterio/meter-pov/chain"
	"github.com/meterio/meter-pov/meter"
	"github.com/meterio/meter-pov/powpool"
)

const (
	RoundInterval            = 1500 * time.Millisecond
	RoundTimeoutInterval     = 12 * time.Second // update timeout from 16 to 12 secs.
	RoundTimeoutLongInterval = 21 * time.Second // update timeout from 40 to 21 secs

	MIN_MBLOCKS_AN_EPOCH = uint32(4)
)

type Pacemaker struct {
	reactor     *Reactor //global reactor info
	proposalMap *ProposalMap
	logger      log15.Logger
	chain       *chain.Chain

	// Current round (current_round - highest_qc_round determines the timeout).
	// Current round is basically max(highest_qc_round, highest_received_tc, highest_local_tc) + 1
	// update_current_round take care of updating current_round and sending new round event if
	// it changes
	currentRound    uint32
	mainLoopStarted bool
	minMBlocks      uint32

	// Utility data structures
	qcVoteManager *QCVoteManager
	tcVoteManager *TCVoteManager

	// HotStuff fields
	lastVotingHeight uint32
	lastVoteMsg      *PMVoteMessage
	QCHigh           *draftQC
	blockLocked      *draftBlock

	lastOnBeatRound int32

	// Channels
	roundTimeoutCh chan PMRoundTimeoutInfo
	cmdCh          chan PMCmd
	beatCh         chan PMBeatInfo

	// Timeout
	roundTimer     *time.Timer
	TCHigh         *TimeoutCert
	timeoutCounter uint64
}

func NewPacemaker(r *Reactor) *Pacemaker {
	p := &Pacemaker{
		reactor: r,
		logger:  log15.New("pkg", "pacer"),
		chain:   r.chain,

		qcVoteManager: NewQCVoteManager(r.blsCommon.System, r.committeeSize),
		tcVoteManager: NewTCVoteManager(r.blsCommon.System, r.committeeSize),

		cmdCh:           make(chan PMCmd, 2),
		beatCh:          make(chan PMBeatInfo, 2),
		roundTimeoutCh:  make(chan PMRoundTimeoutInfo, 2),
		roundTimer:      nil,
		proposalMap:     NewProposalMap(r.chain),
		timeoutCounter:  0,
		lastOnBeatRound: -1,
	}
	return p
}

func (p *Pacemaker) CreateLeaf(parent *draftBlock, justify *draftQC, round uint32) (error, *draftBlock) {
	p.logger.Info(fmt.Sprintf("CreateLeaf: round=%v, QC(H:%v,R:%v), Parent(H:%v,R:%v)", round, justify.QC.QCHeight, justify.QC.QCRound, parent.Height, parent.Round))
	timeout := p.TCHigh != nil
	parentBlock := parent.ProposedBlock
	if parentBlock == nil {
		return ErrParentBlockEmpty, nil
	}
	proposeKBlock := false
	var powResults *powpool.PowResult
	if (parentBlock.Number()+1-parentBlock.LastKBlockHeight()) >= p.minMBlocks && !timeout {
		proposeKBlock, powResults = powpool.GetGlobPowPoolInst().GetPowDecision()
	}

	proposeStopCommitteeBlock := (parentBlock.BlockType() == block.BLOCK_TYPE_K_BLOCK)

	// propose appropriate block info
	if proposeStopCommitteeBlock {
		return p.buildStopCommitteeBlock(parent, justify, round)
	} else if proposeKBlock {
		kblockData := &block.KBlockData{Nonce: uint64(powResults.Nonce), Data: powResults.Raw}
		rewards := powResults.Rewards
		return p.buildKBlock(parent, justify, round, kblockData, rewards)
	} else {
		if p.reactor.curEpoch != 0 && round != 0 && round <= justify.QC.QCRound {
			p.logger.Warn("Invalid round to propose", "round", round, "qcRound", justify.QC.QCRound)
			return ErrInvalidRound, nil
		}
		if p.reactor.curEpoch != 0 && round != 0 && round <= parent.Round {
			p.logger.Warn("Invalid round to propose", "round", round, "parentRound", parent.Round)
			return ErrInvalidRound, nil
		}
		return p.buildMBlock(parent, justify, round)
	}
}

// b_exec <- b_lock <- b <- b' <- bnew*
func (p *Pacemaker) Update(bnew *draftBlock) error {

	var block, blockPrime *draftBlock
	//now pipeline full, roll this pipeline first
	blockPrime = bnew.Justify.QCNode
	if blockPrime == nil {
		p.logger.Warn("blockPrime is empty, early termination of Update")
		return nil
	}
	if blockPrime.Committed {
		p.logger.Warn("b' is commited", "b'", blockPrime.ProposedBlock.ShortID())
		return nil
	}
	block = blockPrime.Justify.QCNode
	if block.Committed {
		p.logger.Warn("b is committed", "b", block.ProposedBlock.ShortID())
	}
	if block == nil {
		//bnew Justify is already higher than current QCHigh
		p.UpdateQCHigh(bnew.Justify)
		p.logger.Warn("block is empty, early termination of Update")
		return nil
	}

	p.logger.Debug(fmt.Sprintf("bnew = %v", bnew.ToString()))
	p.logger.Debug(fmt.Sprintf("b'   = %v", blockPrime.ToString()))
	p.logger.Debug(fmt.Sprintf("b    = %v", block.ToString()))

	// pre-commit phase on b"
	p.UpdateQCHigh(bnew.Justify)

	/* commit requires direct parent */
	if blockPrime.Parent != block {
		return nil
	}

	commitReady := []commitReadyBlock{}
	for b := blockPrime; b.Parent.Height > p.blockLocked.Height; b = b.Parent {
		// XXX: b must be prepended the slice, so we can commit blocks in order
		commitReady = append([]commitReadyBlock{{block: b.Parent, escortQC: b.ProposedBlock.QC}}, commitReady...)
	}
	p.OnCommit(commitReady)

	p.blockLocked = block // commit phase on b
	return nil
}

func (p *Pacemaker) OnCommit(commitReady []commitReadyBlock) {
	for _, b := range commitReady {

		blk := b.block
		escortQC := b.escortQC

		if blk == nil {
			p.logger.Warn("skip commit empty block")
			continue
		}

		// TBD: how to handle this case???
		if !blk.SuccessProcessed {
			p.logger.Error("process this proposal failed, possible my states are wrong", "height", blk.Height, "round", blk.Round, "action", "commit", "err", blk.ProcessError)
			continue
		}
		if blk.ProcessError == errKnownBlock {
			p.logger.Warn("skip commit known block", "height", blk.Height, "round", blk.Round)
			continue
		}
		// commit the approved block
		err := p.commitBlock(blk, escortQC)
		if err != nil {
			if err != chain.ErrBlockExist && err != errKnownBlock {
				if blk != nil {
					p.logger.Warn("commit failed !!!", "err", err, "blk", blk.ProposedBlock.CompactString())
				} else {
					p.logger.Warn("commit failed !!!", "err", err)
				}
				//revert to checkpoint
				best := p.reactor.chain.BestBlock()
				state, err := p.reactor.stateCreator.NewState(best.Header().StateRoot())
				if err != nil {
					panic(fmt.Sprintf("revert the state faild ... %v", err))
				}
				state.RevertTo(blk.CheckPoint)
			} else {
				if blk != nil && blk.ProposedBlock != nil {
					p.logger.Debug(fmt.Sprintf("block %d already in chain", blk.ProposedBlock.Number()), "id", blk.ProposedBlock.ShortID())
				} else {
					p.logger.Info("block alreday in chain")
				}
			}
		}

		if blk.BlockType == KBlockType {
			p.logger.Info("committed a kblock, stop pacemaker", "height", blk.Height, "round", blk.Round)
			p.SendEpochEndInfo(blk)
			// p.Stop()
		}

		// BUG FIX: normally proposal message are cleaned once it is committed. It is ok because this proposal
		// is not needed any more. Only in one case, if somebody queries the more old message, we can not give.
		// so proposals are kept in this committee and clean all of them at the stopping of pacemaker.
		// remove this draftBlock from map.
		//delete(p.proposalMap, b.Height)
		p.proposalMap.Prune(blk)
	}
}

func (p *Pacemaker) OnReceiveProposal(mi *IncomingMsg) {
	msg := mi.Msg.(*PMProposalMessage)
	height := msg.Height
	round := msg.Round

	// drop outdated proposal
	if height < p.blockLocked.Height {
		p.logger.Info("outdated proposal (height <= bLocked.height), dropped ...", "height", height, "bLocked.height", p.blockLocked.Height)
		return
	}

	blk := msg.DecodeBlock()
	qc := blk.QC
	p.logger.Debug("start to handle received block proposal ", "block", blk.Oneliner())

	// address parent
	parent := p.proposalMap.GetOne(msg.ParentHeight, msg.ParentRound, blk.ParentID())
	if parent == nil {
		p.logger.Error("could not get parent draft, throw it back in queue", "height", msg.ParentHeight, "round", msg.ParentRound, "parent", blk.ParentID().ToBlockShortID())
		p.reactor.inQueue.Add(mi)
		return
	}

	qcNode := parent
	// we have qcNode, need to check qcNode and blk.QC is referenced the same
	if match, err := draftBlockMatchQC(qcNode, qc); match && err == nil {
		p.logger.Debug("addressed qcNode ...", "qcHeight", qc.QCHeight, "qcRound", qc.QCRound)
	} else {
		p.logger.Error("parent doesn't match qc from proposal, potential fork happens...", "qcHeight", qc.QCHeight, "qcRound", qc.QCRound, "parent", parent.ProposedBlock.ID().ToBlockShortID())

		// Theoratically, this should not be worrisome anymore, since the parent is addressed by blockID
		// instead of addressing proposal by height, we already supported the fork in proposal space
		// so if the qc doesn't match parent proposal known to me, cases are:
		// 1. I don't have the correct parent, I will assume that others to commit to the right one and i'll do nothing
		// 2. The current proposal is invalid and I should not vote
		// in both cases, I should wait instead of sending messages to confuse peers
		return
	}

	// create justify node
	justify := newPMQuorumCert(qc, qcNode)

	// revert the proposals if I'm not the round proposer and I received a proposal with a valid TC
	validTimeout := p.verifyTimeoutCert(msg.TimeoutCert, msg.Round)

	// place the current proposal in proposal space
	if p.proposalMap.GetByID(blk.ID()) == nil {
		p.proposalMap.Add(&draftBlock{
			Height:        height,
			Round:         round,
			Parent:        parent,
			Justify:       justify,
			ProposedBlock: blk,
			RawBlock:      block.BlockEncodeBytes(blk),
			BlockType:     BlockType(blk.BlockType()),
		})
	}

	bnew := p.proposalMap.GetByID(blk.ID())
	if bnew.Height > p.lastVotingHeight && p.IsExtendedFromBLocked(bnew) {
		if validTimeout {
			p.enterRound(bnew.Round, UpdateOnTimeoutCertProposal)
		} else {
			if msg.DecodeBlock().IsKBlock() {
				// if proposed block is KBlock, reset the timer with extra time cushion
				p.enterRound(bnew.Round, UpdateOnKBlockProposal)
			} else {
				p.enterRound(bnew.Round, UpdateOnRegularProposal)
			}
		}

		// parent round must strictly < bnew round
		// justify round must strictly < bnew round
		if justify != nil && bnew != nil && bnew.Justify != nil && bnew.Justify.QC != nil {
			justifyRound := justify.QC.QCRound
			parentRound := bnew.Justify.QC.QCRound
			p.logger.Debug("check round for proposal", "parentRound", parentRound, "justifyRound", justifyRound, "bnewRound", bnew.Round)
			if parentRound > 0 && justifyRound > 0 {
				if parentRound >= bnew.Round {
					p.logger.Error("parent round must strictly < bnew round")
					return
				}
				if justifyRound >= bnew.Round {
					p.logger.Error("justify round must strictly < bnew round")
					return
				}
			}
		}

		if err := p.ValidateProposal(bnew); err != nil {
			p.logger.Error("HELP: Validate Proposal failed", "error", err)
			return
		}

		// vote back only if not in catch-up mode
		msg, err := p.BuildVoteMessage(msg)
		if err != nil {
			p.logger.Error("could not build vote message", "err", err)
			return
		}
		// send vote message to leader
		p.sendMsg(msg, false)
		p.lastVotingHeight = bnew.Height
		p.lastVoteMsg = msg

	}

	p.Update(bnew)
}

func (p *Pacemaker) OnReceiveVote(mi *IncomingMsg) {
	msg := mi.Msg.(*PMVoteMessage)

	height := msg.VoteHeight
	round := msg.VoteRound

	// drop outdated vote
	if round < p.currentRound {
		p.logger.Info("outdated vote, dropped ...", "currentRound", p.currentRound, "voteRound", round)
		return
	}
	if !p.reactor.amIRoundProproser(round + 1) {
		p.logger.Info("invalid vote, I'm not the expected next proposer ...", "round", round)
		return
	}

	b := p.proposalMap.GetOne(height, round, msg.VoteBlockID)
	if b == nil {
		p.logger.Warn("can not get parnet block")
		// return errors.New("can not address block")
		return
	}

	qcFormed := p.qcVoteManager.AddVote(msg.GetSignerIndex(), height, round, msg.VoteBlockID, msg.VoteSignature, msg.VoteHash)
	if !qcFormed {
		return
	}

	//reach 2/3 majority, trigger the pipeline cmd
	qc := p.qcVoteManager.Aggregate(height, round, msg.VoteBlockID, p.reactor.curEpoch)
	if qc == nil {
		p.logger.Warn("could not form qc")
		return
		// return errors.New("could not form QC")
	}
	pmQC := &draftQC{QCNode: b, QC: qc}
	changed := p.UpdateQCHigh(pmQC)
	if changed {
		// if QC is updated, schedule onbeat now
		p.ScheduleOnBeat(p.reactor.curEpoch, qc.QCRound+1, BeatOnHigherQC, 1000*time.Millisecond)
	}
}

func (p *Pacemaker) OnPropose(qc *draftQC, round uint32) {
	parent := p.proposalMap.GetOneByEscortQC(qc.QC)
	err, bnew := p.CreateLeaf(parent, qc, round)
	if err != nil {
		p.logger.Error("could not create leaf", "err", err)
		return
	}
	// proposedBlk := bnew.ProposedBlockInfo.ProposedBlock
	proposedQC := bnew.ProposedBlock.QC

	if bnew.Height <= proposedQC.QCHeight {
		p.logger.Error("proposed block refers to an invalid qc", "proposedQC", proposedQC.QCHeight, "proposedHeight", bnew.Height)
		return
	}

	msg, err := p.BuildProposalMessage(bnew.Height, bnew.Round, bnew, p.TCHigh)
	if err != nil {
		p.logger.Error("could not build proposal message", "err", err)
		return
	}
	p.TCHigh = nil

	// create slot in proposalMap directly, instead of sendmsg to self.
	p.proposalMap.Add(bnew)

	//send proposal to every committee members including myself
	p.sendMsg(msg, true)

}

func (p *Pacemaker) UpdateQCHigh(qc *draftQC) bool {
	updated := false
	oqc := p.QCHigh
	// update local qcHigh if
	// newQC.height > qcHigh.height
	// or newQC.height = qcHigh.height && newQC.round > qcHigh.round
	if qc.QC.QCHeight > p.QCHigh.QC.QCHeight || (qc.QC.QCHeight == p.QCHigh.QCNode.Height && qc.QC.QCRound > p.QCHigh.QCNode.Round) {
		p.QCHigh = qc
		updated = true
	}
	p.logger.Debug("after update QCHigh", "updated", updated, "from", oqc.ToString(), "to", p.QCHigh.ToString())

	return updated
}

func (p *Pacemaker) OnBeat(epoch uint64, round uint32, reason beatReason) {
	// avoid leftover onbeat
	if epoch < p.reactor.curEpoch {
		p.logger.Warn(fmt.Sprintf("outdated onBeat (epoch(%v) < local epoch(%v)), skip ...", epoch, p.reactor.curEpoch))
		return
	}
	// avoid duplicate onbeat
	if epoch == p.reactor.curEpoch && int32(round) <= p.lastOnBeatRound {
		p.logger.Warn(fmt.Sprintf("outdated onBeat (round(%v) <= lastOnBeatRound(%v)), skip ...", round, p.lastOnBeatRound))
		return
	}

	p.lastOnBeatRound = int32(round)
	p.logger.Info("--------------------------------------------------")
	p.logger.Info(fmt.Sprintf("OnBeat Epoch:%v, Round:%v, Reason:%v ", epoch, round, reason.String()))
	p.logger.Info("--------------------------------------------------")
	// parent already got QC, pre-commit it

	//b := p.QCHigh.QCNode
	b := p.proposalMap.GetOneByEscortQC(p.QCHigh.QC)
	if b == nil {
		return
	}

	if reason == BeatOnInit {
		// only reset the round timer at initialization
		p.resetRoundTimer(round, TimerInit)
	}

	if !p.reactor.amIRoundProproser(round) {
		p.logger.Info("I am NOT round proposer", "round", round)
		return
	}

	p.enterRound(round, UpdateOnBeat)
	pmRoleGauge.Set(2) // leader
	p.logger.Info("I AM round proposer", "round", round)

	p.OnPropose(p.QCHigh, round)
}

func (p *Pacemaker) OnReceiveTimeout(mi *IncomingMsg) {
	msg := mi.Msg.(*PMTimeoutMessage)

	// drop invalid msg
	if !p.reactor.amIRoundProproser(msg.WishRound) {
		p.logger.Debug("invalid timeout msg, I'm not the expected proposer", "epoch", msg.Epoch, "wishRound", msg.WishRound)
		return
	}

	tcUpdated := false
	qcUpdated := false

	// collect wish vote to see if TC is formed
	tcFormed := p.tcVoteManager.AddVote(msg.SignerIndex, msg.Epoch, msg.WishRound, msg.WishVoteSig, msg.WishVoteHash)
	if tcFormed {
		tc := p.tcVoteManager.Aggregate(msg.Epoch, msg.WishRound)
		p.TCHigh = tc
		tcUpdated = true
	}

	// collect vote and see if QC is formed
	qcFormed := p.qcVoteManager.AddVote(msg.SignerIndex, msg.LastVoteHeight, msg.LastVoteRound, msg.LastVoteBlockID, msg.LastVoteSignature, msg.LastVoteHash)
	if qcFormed {
		// TODO: new qc formed
		qc := p.qcVoteManager.Aggregate(msg.LastVoteHeight, msg.LastVoteRound, msg.LastVoteBlockID, p.reactor.curEpoch)
		escortQCNode := p.proposalMap.GetOneByEscortQC(qc)
		updated := p.UpdateQCHigh(&draftQC{QCNode: escortQCNode, QC: qc})
		if updated {
			qcUpdated = true
		}
	}

	qc := msg.DecodeQCHigh()
	qcNode := p.proposalMap.GetOneByEscortQC(qc)
	updated := p.UpdateQCHigh(&draftQC{QCNode: qcNode, QC: qc})
	if updated {
		qcUpdated = true
	}

	if qcUpdated {
		p.ScheduleOnBeat(p.reactor.curEpoch, p.QCHigh.QC.QCRound+1, BeatOnHigherQC, 500*time.Millisecond)
	}
	if tcUpdated {
		p.ScheduleOnBeat(p.reactor.curEpoch, p.TCHigh.Round, BeatOnHigherQC, 500*time.Millisecond)
	}
}

// Committee Leader triggers
func (p *Pacemaker) Regulate() {
	p.reactor.PrepareEnvForPacemaker()

	bestQC := p.reactor.chain.BestQC()
	bestBlk, err := p.reactor.chain.GetTrunkBlock(bestQC.QCHeight)
	if err != nil {
		p.logger.Error("could not get bestBlock with bestQC")
		panic("could not get bestBlock with bestQC")
	}

	round := bestQC.QCRound
	actualRound := round + 1
	if bestBlk.IsKBlock() || bestBlk.Number() == 0 {
		// started with KBlock or Genesis
		round = uint32(0)
		actualRound = 0
	}

	p.logger.Info(fmt.Sprintf("*** Pacemaker start with QC %v", bestQC.CompactString()))
	p.lastOnBeatRound = int32(actualRound) - 1
	pmRoleGauge.Set(1) // validator

	// if InitCfgDelegates is set, pacemaker in bootstrap mode
	if !p.reactor.config.InitCfgdDelegates {
		p.minMBlocks = MIN_MBLOCKS_AN_EPOCH
	} else {
		p.minMBlocks = p.reactor.config.EpochMBlockCount
		if meter.IsStaging() {
			log.Info("skip setting InitCfgdDelegates to false in staging")
		} else {
			// toggle it off so it will switch to normal mode next epoch
			p.reactor.config.InitCfgdDelegates = false
		}
	}

	bestNode := p.proposalMap.GetOneByEscortQC(bestQC)
	if bestNode == nil {
		p.logger.Debug("started with empty qcNode")
	}
	qcInit := newPMQuorumCert(bestQC, bestNode)

	// now assign b_lock b_exec, b_leaf qc_high
	p.blockLocked = bestNode
	p.lastVotingHeight = 0
	p.lastVoteMsg = nil
	p.QCHigh = qcInit
	p.proposalMap.Add(bestNode)

	pmRunningGauge.Set(1)

	if !p.mainLoopStarted {
		go p.mainLoop()
	}

	p.ScheduleOnBeat(p.reactor.curEpoch, actualRound, BeatOnInit, 500*time.Microsecond) //delay 0.5s
}

func (p *Pacemaker) ScheduleOnBeat(epoch uint64, round uint32, reason beatReason, d time.Duration) {
	// p.enterRound(round, IncRoundOnBeat)
	time.AfterFunc(d, func() {
		p.beatCh <- PMBeatInfo{epoch, round, reason}
	})
}

func (p *Pacemaker) scheduleRegulate() {
	// schedule Regulate
	// make sure this Regulate cmd is the very next cmd
Regulate:
	for {
		select {
		case <-p.cmdCh:
		default:
			break Regulate
		}
	}

	p.cmdCh <- PMCmdRegulate
}

func (p *Pacemaker) mainLoop() {
	interruptCh := make(chan os.Signal, 1)
	p.mainLoopStarted = true
	// signal.Notify(interruptCh, syscall.SIGINT, syscall.SIGTERM)

	for {
		bestBlock := p.chain.BestBlock()
		if bestBlock.Number() > p.QCHigh.QC.QCHeight {
			//TODO: regulate pacemaker
			p.scheduleRegulate()
		}
		select {
		case ee := <-p.reactor.EpochEndCh:
			if ee.Height < p.reactor.lastKBlockHeight || ee.Nonce == p.reactor.curNonce {
				p.logger.Info("epochEnd handled already, skip for now ...", "height", ee.Height, "nonce", ee.Nonce)
				continue
			}
			p.logger.Info("handle epoch end", "epoch", ee.Epoch, "height", ee.Height, "nonce", ee.Nonce)
			p.scheduleRegulate()
		case cmd := <-p.cmdCh:
			if cmd == PMCmdRegulate {
				p.scheduleRegulate()
			}
		case ti := <-p.roundTimeoutCh:
			p.OnRoundTimeout(ti)
		case b := <-p.beatCh:
			p.OnBeat(b.epoch, b.round, b.reason)
		case m := <-p.reactor.inQueue.queue:
			// if not in committee, skip rcvd messages
			if !p.reactor.inCommittee {
				p.logger.Info("skip handling msg bcuz I'm not in committee", "type", m.Msg.GetType())
				continue
			}
			if m.Msg.GetEpoch() != p.reactor.curEpoch {
				p.logger.Info("rcvd message w/ mismatched epoch ", "epoch", m.Msg.GetEpoch(), "myEpoch", p.reactor.curEpoch, "type", m.Msg.GetType())
				continue
			}
			if time.Now().After(m.ExpireAt) {
				p.logger.Info(fmt.Sprintf("incoming %s msg expired, dropped ...", m.Msg.GetType()))
				continue
			}
			switch m.Msg.(type) {
			case *PMProposalMessage:
				p.OnReceiveProposal(m)
			case *PMVoteMessage:
				p.OnReceiveVote(m)
			case *PMTimeoutMessage:
				p.OnReceiveTimeout(m)
			default:
				p.logger.Warn("received an message in unknown type")
			}

		case <-interruptCh:
			p.logger.Warn("interrupt by user, exit now")
			p.mainLoopStarted = false
			return

		}
	}
}

func (p *Pacemaker) SendEpochEndInfo(b *draftBlock) {
	// clean off chain for next committee.
	blk := b.ProposedBlock
	if blk.IsKBlock() {
		data, _ := blk.GetKBlockData()
		info := EpochEndInfo{
			Height:           blk.Number(),
			LastKBlockHeight: blk.LastKBlockHeight(),
			Nonce:            data.Nonce,
			Epoch:            blk.QC.EpochID,
		}
		p.reactor.EpochEndCh <- info

		p.logger.Info("sent kblock info to reactor", "nonce", info.Nonce, "height", info.Height)
	}
}

func (p *Pacemaker) OnRoundTimeout(ti PMRoundTimeoutInfo) {
	p.logger.Warn(fmt.Sprintf("round %d timeout", ti.round), "counter", p.timeoutCounter)

	updated := p.enterRound(ti.round+1, UpdateOnTimeout)
	newTi := &PMRoundTimeoutInfo{
		height:  p.QCHigh.QC.QCHeight + 1,
		round:   p.currentRound,
		counter: p.timeoutCounter + 1,
	}
	if updated {
		pmRoleGauge.Set(1) // validator

		// send new round msg to next round proposer
		msg, err := p.BuildTimeoutMessage(p.QCHigh, newTi, p.lastVoteMsg)
		if err != nil {
			p.logger.Error("could not build new view message", "err", err)
		} else {
			p.sendMsg(msg, false)
		}
	}
}

func (p *Pacemaker) enterRound(round uint32, reason roundUpdateReason) bool {
	updated := (p.currentRound != round)
	switch reason {
	case UpdateOnBeat:
		fallthrough
	case UpdateOnRegularProposal:
		if round > p.currentRound {
			updated = true
			p.resetRoundTimer(round, TimerInit)
		} else if round == p.currentRound && p.reactor.amIRoundProproser(round) {
			// proposer reset timer when recv proposal
			updated = false
			p.resetRoundTimer(round, TimerInit)
		}
	case UpdateOnKBlockProposal:
		if round > p.currentRound {
			updated = true
			p.resetRoundTimer(round, TimerInitLong)
		} else if round == p.currentRound && p.reactor.amIRoundProproser(round) {
			// proposer reset timer when recv proposal
			updated = false
			p.resetRoundTimer(round, TimerInitLong)
		}
	case UpdateOnTimeoutCertProposal:
		if round >= p.currentRound {
			p.resetRoundTimer(round, TimerInit)
		}
	case UpdateOnTimeout:
		p.resetRoundTimer(round, TimerInc)
	}

	if updated {
		oldRound := p.currentRound
		p.currentRound = round
		proposer := p.reactor.getRoundProposer(round)
		p.logger.Info(fmt.Sprintf("update round %d->%d", oldRound, p.currentRound), "reason", reason.String(), "proposer", proposer.NameWithIP())
		pmRoundGauge.Set(float64(p.currentRound))
		return true
	}
	return false
}

func (p *Pacemaker) startRoundTimer(round uint32, reason roundTimerUpdateReason) {
	if p.roundTimer == nil {
		baseInterval := RoundTimeoutInterval
		switch reason {
		case TimerInitLong:
			baseInterval = RoundTimeoutLongInterval
			p.timeoutCounter = 0
		case TimerInit:
			p.timeoutCounter = 0
		case TimerInc:
			p.timeoutCounter++
		}
		var power uint64 = 0
		if p.timeoutCounter > 1 {
			power = p.timeoutCounter - 1
		}
		timeoutInterval := baseInterval * (1 << power)
		p.logger.Info(fmt.Sprintf("> start round %d timer", round), "interval", int64(timeoutInterval/time.Second), "timeoutCount", p.timeoutCounter)
		p.roundTimer = time.AfterFunc(timeoutInterval, func() {
			p.roundTimeoutCh <- PMRoundTimeoutInfo{round: round, counter: p.timeoutCounter}
		})
	}
}

func (p *Pacemaker) stopRoundTimer() bool {
	if p.roundTimer != nil {
		p.logger.Debug(fmt.Sprintf("stop timer for round %d", p.currentRound))
		p.roundTimer.Stop()
		p.roundTimer = nil
	}
	return true
}

func (p *Pacemaker) resetRoundTimer(round uint32, reason roundTimerUpdateReason) {
	p.stopRoundTimer()
	p.startRoundTimer(round, reason)
}
