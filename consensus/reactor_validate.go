// Copyright (c) 2020 The Meter.io developers

// Distributed under the GNU Lesser General Public License v3.0 software license, see the accompanying
// file LICENSE or <https://www.gnu.org/licenses/lgpl-3.0.html>

// Copyright (c) 2020 The Meter.io developers

// Distributed under the GNU Lesser General Public License v3.0 software license, see the accompanying
// file LICENSE or <https://www.gnu.org/licenses/lgpl-3.0.html>

package consensus

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/rlp"
	"github.com/inconshreveable/log15"

	"github.com/pkg/errors"

	"github.com/meterio/meter-pov/block"
	"github.com/meterio/meter-pov/consensus/governor"
	"github.com/meterio/meter-pov/meter"
	"github.com/meterio/meter-pov/powpool"
	"github.com/meterio/meter-pov/runtime"
	"github.com/meterio/meter-pov/script"
	"github.com/meterio/meter-pov/script/accountlock"
	"github.com/meterio/meter-pov/script/auction"
	"github.com/meterio/meter-pov/script/staking"
	"github.com/meterio/meter-pov/state"
	"github.com/meterio/meter-pov/tx"
	"github.com/meterio/meter-pov/xenv"
)

var log = log15.New("pkg", "consensus")

// Process process a block.
func (c *ConsensusReactor) ProcessSyncedBlock(blk *block.Block, nowTimestamp uint64) (*state.Stage, tx.Receipts, error) {
	header := blk.Header()

	if _, err := c.chain.GetBlockHeader(header.ID()); err != nil {
		if !c.chain.IsNotFound(err) {
			return nil, nil, err
		}
	} else {
		// we may already have this blockID. If it is after the best, still accept it
		if header.Number() <= c.chain.BestBlock().Number() {
			return nil, nil, errKnownBlock
		} else {
			c.logger.Debug("continue to process blk ...", "height", header.Number())
		}
	}

	parentHeader, err := c.chain.GetBlockHeader(header.ParentID())
	if err != nil {
		if !c.chain.IsNotFound(err) {
			return nil, nil, err
		}
		return nil, nil, errParentMissing
	}

	state, err := c.stateCreator.NewState(parentHeader.StateRoot())
	if err != nil {
		return nil, nil, err
	}

	stage, receipts, err := c.validate(state, blk, parentHeader, nowTimestamp, false)
	if err != nil {
		return nil, nil, err
	}

	return stage, receipts, nil
}

func (c *ConsensusReactor) ProcessProposedBlock(parentHeader *block.Header, blk *block.Block, nowTimestamp uint64) (*state.Stage, tx.Receipts, error) {
	header := blk.Header()

	if _, err := c.chain.GetBlockHeader(header.ID()); err != nil {
		if !c.chain.IsNotFound(err) {
			return nil, nil, err
		}
	} else {
		return nil, nil, errKnownBlock
	}

	if parentHeader == nil {
		return nil, nil, errParentHeaderMissing
	}

	state, err := c.stateCreator.NewState(parentHeader.StateRoot())
	if err != nil {
		return nil, nil, err
	}

	stage, receipts, err := c.validate(state, blk, parentHeader, nowTimestamp, true)
	if err != nil {
		return nil, nil, err
	}

	return stage, receipts, nil
}

func (c *ConsensusReactor) validate(
	state *state.State,
	block *block.Block,
	parentHeader *block.Header,
	nowTimestamp uint64,
	forceValidate bool,
) (*state.Stage, tx.Receipts, error) {
	header := block.Header()

	epoch := block.GetBlockEpoch()

	if err := c.validateBlockHeader(header, parentHeader, nowTimestamp, forceValidate, epoch); err != nil {
		return nil, nil, err
	}

	if err := c.validateProposer(header, parentHeader, state); err != nil {
		return nil, nil, err
	}

	if err := c.validateBlockBody(block, forceValidate); err != nil {
		return nil, nil, err
	}

	stage, receipts, err := c.verifyBlock(block, state, forceValidate)
	if err != nil {
		return nil, nil, err
	}

	return stage, receipts, nil
}

func (c *ConsensusReactor) validateBlockHeader(header *block.Header, parent *block.Header, nowTimestamp uint64, forceValidate bool, epoch uint64) error {
	if header.Timestamp() <= parent.Timestamp() {
		return consensusError(fmt.Sprintf("block timestamp behind parents: parent %v, current %v", parent.Timestamp(), header.Timestamp()))
	}

	if header.Timestamp() > nowTimestamp+meter.BlockInterval {
		return errFutureBlock
	}

	if !block.GasLimit(header.GasLimit()).IsValid(parent.GasLimit()) {
		return consensusError(fmt.Sprintf("block gas limit invalid: parent %v, current %v", parent.GasLimit(), header.GasLimit()))
	}

	if header.GasUsed() > header.GasLimit() {
		return consensusError(fmt.Sprintf("block gas used exceeds limit: limit %v, used %v", header.GasLimit(), header.GasUsed()))
	}

	if header.TotalScore() <= parent.TotalScore() {
		return consensusError(fmt.Sprintf("block total score invalid: parent %v, current %v", parent.TotalScore(), header.TotalScore()))
	}

	if epoch != meter.KBlockEpoch && header.LastKBlockHeight() < parent.LastKBlockHeight() {
		return consensusError(fmt.Sprintf("block LastKBlockHeight invalid: parent %v, current %v", parent.LastKBlockHeight(), header.LastKBlockHeight()))
	}

	if forceValidate && header.LastKBlockHeight() != c.lastKBlockHeight {
		return consensusError(fmt.Sprintf("header LastKBlockHeight invalid: header %v, local %v", header.LastKBlockHeight(), c.lastKBlockHeight))
	}

	return nil
}

func (c *ConsensusReactor) validateProposer(header *block.Header, parent *block.Header, st *state.State) error {
	_, err := header.Signer()
	if err != nil {
		return consensusError(fmt.Sprintf("block signer unavailable: %v", err))
	}
	// fmt.Println("signer", signer)
	return nil
}

func (c *ConsensusReactor) validateBlockBody(blk *block.Block, forceValidate bool) error {
	header := blk.Header()
	txs := blk.Transactions()
	if header.TxsRoot() != txs.RootHash() {
		return consensusError(fmt.Sprintf("block txs root mismatch: want %v, have %v", header.TxsRoot(), txs.RootHash()))
	}
	if blk.GetMagic() != block.BlockMagicVersion1 {
		return consensusError(fmt.Sprintf("block magic mismatch, has %v, expect %v", blk.GetMagic(), block.BlockMagicVersion1))
	}

	txUniteHashes := make(map[meter.Bytes32]int)
	txClauseIds := make(map[meter.Bytes32]int)
	scriptHeaderIds := make(map[meter.Bytes32]int)
	scriptBodyIds := make(map[meter.Bytes32]int)
	rinfoIds := make(map[meter.Bytes32]int)

	rewardTxs := tx.Transactions{}

	parentBlock, err := c.chain.GetBlock(header.ParentID())
	if err != nil {
		panic("get parentBlock failed")
	}
	if blk.IsKBlock() {
		best := parentBlock
		chainTag := c.chain.Tag()
		bestNum := c.chain.BestBlock().Number()
		curEpoch := uint32(c.curEpoch)
		// distribute the base reward
		state, err := c.stateCreator.NewState(c.chain.BestBlock().Header().StateRoot())
		if err != nil {
			panic("get state failed")
		}

		proposalKBlock, powResults := powpool.GetGlobPowPoolInst().GetPowDecision()
		if proposalKBlock && forceValidate {
			rewards := powResults.Rewards
			// Build.
			fmt.Println("---------------- Local Build Reward Txs for validation ----------------")
			rewardTxs = c.buildKBlockTxs(parentBlock, rewards, chainTag, bestNum, curEpoch, best, state)
			fmt.Println("---------------- End of Local Build Reward Txs ----------------", "txs", len(rewardTxs))
			for _, tx := range rewardTxs {
				fmt.Println("hash:", tx.ID().String(), "uniteHash:", tx.UniteHash().String())
			}

			// Decode.
			for _, rewardTx := range rewardTxs {
				rewardTxUniteHash := rewardTx.UniteHash()
				if _, ok := txUniteHashes[rewardTxUniteHash]; ok {
					txUniteHashes[rewardTxUniteHash] += 1
				} else {
					txUniteHashes[rewardTxUniteHash] = 1
				}

				for _, clause := range rewardTx.Clauses() {
					clauseUniteHash := clause.UniteHash()
					if _, ok := txClauseIds[clauseUniteHash]; ok {
						txClauseIds[clauseUniteHash] += 1
					} else {
						txClauseIds[clauseUniteHash] = 1
					}

					if (clause.Value().Sign() == 0) && (len(clause.Data()) > runtime.MinScriptEngDataLen) && runtime.ScriptEngineCheck(clause.Data()) {
						data := clause.Data()[4:]
						if bytes.Compare(data[:len(script.ScriptPattern)], script.ScriptPattern[:]) != 0 {
							err := fmt.Errorf("Pattern mismatch, pattern = %v", hex.EncodeToString(data[:len(script.ScriptPattern)]))
							fmt.Println(err)
							//return nil, gas, err
						}
						scriptStruct, err := script.ScriptDecodeFromBytes(data[len(script.ScriptPattern):])
						if err != nil {
							fmt.Println("Decode script message failed", err)
							//return nil, gas, err
						}

						scriptHeader := scriptStruct.Header
						scriptHeaderUniteHash := scriptHeader.UniteHash()
						if _, ok := scriptHeaderIds[scriptHeaderUniteHash]; ok {
							scriptHeaderIds[scriptHeaderUniteHash] += 1
						} else {
							scriptHeaderIds[scriptHeaderUniteHash] = 1
						}
						scriptPayload := scriptStruct.Payload

						switch scriptHeader.ModID {
						case script.STAKING_MODULE_ID:
							sb, err := staking.StakingDecodeFromBytes(scriptPayload)
							if err != nil {
								log.Error("Decode StakingDecodeFromBytes script message failed", "error", err)
								//return nil, gas, err
							}

							switch sb.Opcode {
							case staking.OP_GOVERNING:
								if meter.IsTeslaFork6(parentBlock.Number()) {

									rinfo := []*meter.RewardInfoV2{}
									err = rlp.DecodeBytes(sb.ExtraData, &rinfo)
									log.Info("rewardTx rinfo")
									for _, d := range rinfo {
										rinfoIds[d.UniteHash()] = 1
									}
								} else {
									rinfo := []*meter.RewardInfo{}
									err = rlp.DecodeBytes(sb.ExtraData, &rinfo)
									log.Info("rewardTx rinfo")
									for _, d := range rinfo {
										rinfoIds[d.UniteHash()] = 1
									}
								}
							default:
								sbUniteHash := sb.UniteHash()
								if _, ok := scriptBodyIds[sbUniteHash]; ok {
									scriptBodyIds[sbUniteHash] += 1
								} else {
									scriptBodyIds[sbUniteHash] = 1
								}
							}
						case script.AUCTION_MODULE_ID:
							sb, err := auction.AuctionDecodeFromBytes(scriptPayload)
							if err != nil {
								log.Error("Decode AUCTION_MODULE_ID script message failed", "error", err)
								//return nil, gas, err
							}

							sbUniteHash := sb.UniteHash()
							if _, ok := scriptBodyIds[sbUniteHash]; ok {
								scriptBodyIds[sbUniteHash] += 1
							} else {
								scriptBodyIds[sbUniteHash] = 1
							}
						case script.ACCOUNTLOCK_MODULE_ID:
							sb, err := accountlock.AccountLockDecodeFromBytes(scriptPayload)
							if err != nil {
								log.Error("Decode ACCOUNTLOCK_MODULE_ID script message failed", "error", err)
								//return nil, gas, err
							}

							sbUniteHash := sb.UniteHash()
							if _, ok := scriptBodyIds[sbUniteHash]; ok {
								scriptBodyIds[sbUniteHash] += 1
							} else {
								scriptBodyIds[sbUniteHash] = 1
							}
						}
					}
				}
			}
		}
	}

	for _, tx := range txs {
		signer, err := tx.Signer()
		if err != nil {
			return consensusError(fmt.Sprintf("tx signer unavailable: %v", err))
		}

		if forceValidate {
			if _, err = tx.EthTxValidate(); err != nil {
				return err
			}
		}

		// transaction critiers:
		// 1. no signature (no signer)
		// 2. only located in kblock.
		if signer.IsZero() {
			if !blk.IsKBlock() {
				return consensusError(fmt.Sprintf("tx signer unavailable"))
			}

			if forceValidate {
				log.Info("validating tx", "hash", tx.ID().String(), "uniteHash", tx.UniteHash().String())

				// Validate.
				txUniteHash := tx.UniteHash()
				if _, ok := txUniteHashes[txUniteHash]; !ok {
					for index, rewardTx := range rewardTxs {
						log.Info(fmt.Sprintf("rewardTx-tx %v not unavailable, %v", index, rewardTx))
					}
					log.Error(fmt.Sprintf("tx-rewardTx unavailable, %v", tx))

					return consensusError(fmt.Sprintf("minerTx unavailable"))
				}
				txUniteHashes[txUniteHash] -= 1

				for _, clause := range tx.Clauses() {
					clauseUniteHash := clause.UniteHash()

					if _, ok := txClauseIds[clauseUniteHash]; !ok {
						return consensusError(fmt.Sprintf("minerTx clause unavailable"))
					}
					txClauseIds[clauseUniteHash] -= 1

					// Decode.
					if (clause.Value().Sign() == 0) && (len(clause.Data()) > runtime.MinScriptEngDataLen) && runtime.ScriptEngineCheck(clause.Data()) {
						data := clause.Data()[4:]
						if bytes.Compare(data[:len(script.ScriptPattern)], script.ScriptPattern[:]) != 0 {
							err := fmt.Errorf("Pattern mismatch, pattern = %v", hex.EncodeToString(data[:len(script.ScriptPattern)]))
							return consensusError(err.Error())
						}

						scriptStruct, err := script.ScriptDecodeFromBytes(data[len(script.ScriptPattern):])
						if err != nil {
							fmt.Println("Decode script message failed", err)
							return consensusError(err.Error())
						}

						scriptHeader := scriptStruct.Header

						scriptHeaderUniteHash := scriptHeader.UniteHash()
						if _, ok := scriptHeaderIds[scriptHeaderUniteHash]; !ok {
							return consensusError(fmt.Sprintf("minerTx scriptHeader unavailable"))
						}
						scriptHeaderIds[scriptHeaderUniteHash] -= 1

						scriptPayload := scriptStruct.Payload
						switch scriptHeader.ModID {
						case script.STAKING_MODULE_ID:
							sb, err := staking.StakingDecodeFromBytes(scriptPayload)
							if err != nil {
								log.Error("Decode script message failed", "error", err)
								//return nil, gas, err
							}

							switch sb.Opcode {
							case staking.OP_GOVERNING:
								if meter.IsTeslaFork6(parentBlock.Number()) {
									minerTxRinfo := make([]*meter.RewardInfoV2, 0)
									err = rlp.DecodeBytes(sb.ExtraData, &minerTxRinfo)

									fmt.Sprintf("minerTx rinfo")
									for _, d := range minerTxRinfo {
										dUniteHash := d.UniteHash()
										if _, ok := rinfoIds[dUniteHash]; !ok {
											return consensusError(fmt.Sprintf("d.Address %v not exists", d.Address))
										}
										rinfoIds[dUniteHash] -= 1
									}
								} else {
									minerTxRinfo := make([]*meter.RewardInfo, 0)
									err = rlp.DecodeBytes(sb.ExtraData, &minerTxRinfo)

									fmt.Sprintf("minerTx rinfo")
									for _, d := range minerTxRinfo {
										dUniteHash := d.UniteHash()
										if _, ok := rinfoIds[dUniteHash]; !ok {
											return consensusError(fmt.Sprintf("d.Address %v not exists", d.Address))
										}
										rinfoIds[dUniteHash] -= 1
									}
								}
							default:
								sbUniteHash := sb.UniteHash()
								if _, ok := scriptBodyIds[sbUniteHash]; !ok {
									return consensusError(fmt.Sprintf("minerTx STAKING scriptBody unavailable, sb %v", sb))
								}
								scriptBodyIds[sbUniteHash] -= 1
							}
						case script.AUCTION_MODULE_ID:
							sb, err := auction.AuctionDecodeFromBytes(scriptPayload)
							if err != nil {
								log.Error("Decode script message failed", "error", err)
								return consensusError(err.Error())
							}
							//_ = ab
							//scriptBodyIds[ab.UniteHash()] = true
							sbUniteHash := sb.UniteHash()
							if _, ok := scriptBodyIds[sbUniteHash]; !ok {
								return consensusError(fmt.Sprintf("minerTx AUCTION scriptBody unavailable, sb %v", sb))
							}
							scriptBodyIds[sbUniteHash] -= 1
						case script.ACCOUNTLOCK_MODULE_ID:
							sb, err := accountlock.AccountLockDecodeFromBytes(scriptPayload)
							if err != nil {
								log.Error("Decode script message failed", "error", err)
								return consensusError(err.Error())
							}
							//_ = ab
							//scriptBodyIds[ab.UniteHash()] = true
							sbUniteHash := sb.UniteHash()
							if _, ok := scriptBodyIds[sbUniteHash]; !ok {
								return consensusError(fmt.Sprintf("minerTx ACCOUNTLOCK scriptBody unavailable, %v", sb))
							}
							scriptBodyIds[sbUniteHash] -= 1
						}
					}
				}

				// log.Info("end validateBlockBody forceValidate")
			}
		}

		switch {
		case tx.ChainTag() != c.chain.Tag():
			return consensusError(fmt.Sprintf("tx chain tag mismatch: want %v, have %v", c.chain.Tag(), tx.ChainTag()))
		case header.Number() < tx.BlockRef().Number():
			return consensusError(fmt.Sprintf("tx ref future block: ref %v, current %v", tx.BlockRef().Number(), header.Number()))
		case tx.IsExpired(header.Number()):
			return consensusError(fmt.Sprintf("tx expired: ref %v, current %v, expiration %v", tx.BlockRef().Number(), header.Number(), tx.Expiration()))
			// case tx.HasReservedFields():
			// return consensusError(fmt.Sprintf("tx reserved fields not empty"))
		}
	}

	if len(txUniteHashes) != 0 {
		for key, value := range txUniteHashes {
			if value != 0 {
				return consensusError(fmt.Sprintf("local-built txs have %v more tx with uniteHash: %v", value, key))
			}
		}
	}

	if len(txClauseIds) != 0 {
		for key, value := range txClauseIds {
			if value < 0 {
				return consensusError(fmt.Sprintf("local-built txs have %v more clause with uniteHash: %v", value, key))
			}
		}
	}

	if len(scriptHeaderIds) != 0 {
		for key, value := range scriptHeaderIds {
			if value != 0 {
				return consensusError(fmt.Sprintf("local-built txs have %v more scriptHead with uniteHash: %v", value, key))
			}
		}
	}

	if len(scriptBodyIds) != 0 {
		for key, value := range scriptBodyIds {
			if value != 0 {
				return consensusError(fmt.Sprintf("local-built txs have %v more scriptBody with uniteHash: %v", value, key))
			}
		}
	}

	if len(rinfoIds) != 0 {
		for key, value := range rinfoIds {
			if value != 0 {
				return consensusError(fmt.Sprintf("local-built governing tx has %v extraData with uniteHash: %v", value, key))
			}
		}
	}

	return nil
}

func (c *ConsensusReactor) verifyBlock(blk *block.Block, state *state.State, forceValidate bool) (*state.Stage, tx.Receipts, error) {
	var totalGasUsed uint64
	txs := blk.Transactions()
	receipts := make(tx.Receipts, 0, len(txs))
	processedTxs := make(map[meter.Bytes32]bool)
	header := blk.Header()
	signer, _ := header.Signer()
	rt := runtime.New(
		c.chain.NewSeeker(header.ParentID()),
		state,
		&xenv.BlockContext{
			Beneficiary: header.Beneficiary(),
			Signer:      signer,
			Number:      header.Number(),
			Time:        header.Timestamp(),
			GasLimit:    header.GasLimit(),
			TotalScore:  header.TotalScore(),
		})

	findTx := func(txID meter.Bytes32) (found bool, reverted bool, err error) {
		if reverted, ok := processedTxs[txID]; ok {
			return true, reverted, nil
		}
		meta, err := c.chain.GetTransactionMeta(txID, header.ParentID())
		if err != nil {
			if c.chain.IsNotFound(err) {
				return false, false, nil
			}
			return false, false, err
		}
		return true, meta.Reverted, nil
	}

	if forceValidate && blk.IsKBlock() {
		if err := c.verifyKBlock(); err != nil {
			return nil, nil, err
		}
	}

	for _, tx := range txs {
		// Mint transaction critiers:
		// 1. no signature (no signer)
		// 2. only located in 1st transaction in kblock.
		signer, err := tx.Signer()
		if err != nil {
			return nil, nil, consensusError(fmt.Sprintf("tx signer unavailable: %v", err))
		}

		if signer.IsZero() {
			//TBD: check to addresses in clauses
			if !blk.IsKBlock() {
				return nil, nil, consensusError(fmt.Sprintf("tx signer unavailable"))
			}
		}

		// check if tx existed
		if found, _, err := findTx(tx.ID()); err != nil {
			return nil, nil, err
		} else if found {
			return nil, nil, consensusError("tx already exists")
		}

		// check depended tx
		if dep := tx.DependsOn(); dep != nil {
			found, reverted, err := findTx(*dep)
			if err != nil {
				return nil, nil, err
			}
			if !found {
				return nil, nil, consensusError("tx dep broken")
			}

			if reverted {
				return nil, nil, consensusError("tx dep reverted")
			}
		}

		receipt, err := rt.ExecuteTransaction(tx)
		if err != nil {
			return nil, nil, err
		}

		totalGasUsed += receipt.GasUsed
		receipts = append(receipts, receipt)
		processedTxs[tx.ID()] = receipt.Reverted
	}

	if header.GasUsed() != totalGasUsed {
		return nil, nil, consensusError(fmt.Sprintf("block gas used mismatch: want %v, have %v", header.GasUsed(), totalGasUsed))
	}

	receiptsRoot := receipts.RootHash()
	if header.ReceiptsRoot() != receiptsRoot {
		return nil, nil, consensusError(fmt.Sprintf("block receipts root mismatch: want %v, have %v", header.ReceiptsRoot(), receiptsRoot))
	}

	if err := rt.Seeker().Err(); err != nil {
		return nil, nil, errors.WithMessage(err, "chain")
	}

	stage := state.Stage()
	stateRoot, err := stage.Hash()
	if err != nil {
		return nil, nil, err
	}

	if blk.Header().StateRoot() != stateRoot {
		return nil, nil, consensusError(fmt.Sprintf("block state root mismatch: want %v, have %v", header.StateRoot(), stateRoot))
	}

	return stage, receipts, nil
}

func (c *ConsensusReactor) verifyKBlock() error {
	p := powpool.GetGlobPowPoolInst()
	if !p.VerifyNPowBlockPerEpoch() {
		return errors.New("NPowBlockPerEpoch err")
	}

	return nil
}

func (conR *ConsensusReactor) buildKBlockTxs(parentBlock *block.Block, rewards []powpool.PowReward, chainTag byte, bestNum uint32, curEpoch uint32, best *block.Block, state *state.State) tx.Transactions {
	// build miner meter reward
	txs := governor.BuildMinerRewardTxs(rewards, chainTag, bestNum)
	for _, tx := range txs {
		conR.logger.Info("Built miner reward tx: ", "hash", tx.ID().String(), "clauses-size", len(tx.Clauses()))
	}

	lastKBlockHeight := parentBlock.LastKBlockHeight()

	// edison not support the staking/auciton/slashing
	if meter.IsTesla(parentBlock.Number()) {
		stats, err := governor.ComputeStatistics(lastKBlockHeight, parentBlock.Number(), conR.chain, conR.curCommittee, conR.curActualCommittee, conR.csCommon, conR.csPacemaker.calcStatsTx, uint32(conR.curEpoch))
		if err != nil {
			// TODO: do something about this
			conR.logger.Info("no slash statistics need to info", "error", err)
		}
		if len(stats) != 0 {
			statsTx := governor.BuildStatisticsTx(stats, chainTag, bestNum, curEpoch)
			conR.logger.Info("Built stats tx: ", "hash", statsTx.ID().String(), "clauses-size", len(statsTx.Clauses()))
			txs = append(txs, statsTx)
		}

		reservedPrice := GetAuctionReservedPrice()
		initialRelease := GetAuctionInitialRelease()

		if tx := governor.BuildAuctionControlTx(uint64(best.Number()+1), uint64(best.GetBlockEpoch()+1), chainTag, bestNum, initialRelease, reservedPrice, conR.chain); tx != nil {
			conR.logger.Info("Built auction control tx: ", "hash", tx.ID().String(), "clauses-size", len(tx.Clauses()))
			txs = append(txs, tx)
		}

		// exception for staging env
		// build governing tx && autobid tx only when staking delegates is used
		if meter.IsStaging() || conR.sourceDelegates != fromDelegatesFile {
			benefitRatio := governor.GetValidatorBenefitRatio(state)
			validatorBaseReward := governor.GetValidatorBaseRewards(state)
			epochBaseReward := governor.ComputeEpochBaseReward(validatorBaseReward)
			nDays := meter.NDays
			nAuctionPerDay := meter.NEpochPerDay // wrong number before hardfork
			nDays = meter.NDaysV2
			nAuctionPerDay = meter.NAuctionPerDay
			epochTotalReward, err := governor.ComputeEpochTotalReward(benefitRatio, nDays, nAuctionPerDay)
			if err != nil {
				epochTotalReward = big.NewInt(0)
			}
			var rewardMap governor.RewardMap
			if meter.IsTeslaFork2(parentBlock.Number()) {
				fmt.Println("Compute reward map V3")
				if meter.IsStaging() {
					// use staking delegates for calculation during staging
					delegates, _ := conR.getDelegatesFromStaking()
					if err != nil {
						fmt.Println("could not get delegates from staking")
					}
					fmt.Println("Got delegates: ", len(delegates))

					// skip member check for delegates in ComputeRewardMapV3
					rewardMap, err = governor.ComputeRewardMap(epochBaseReward, epochTotalReward, delegates, true)
				} else {
					rewardMap, err = governor.ComputeRewardMapV3(epochBaseReward, epochTotalReward, conR.curDelegates.Delegates, conR.curCommittee.Validators)
				}
			} else {
				fmt.Println("Compute reward map v2")
				rewardMap, err = governor.ComputeRewardMapV2(epochBaseReward, epochTotalReward, conR.curDelegates.Delegates, conR.curCommittee.Validators)
			}

			if err == nil && len(rewardMap) > 0 {
				if meter.IsTeslaFork6(parentBlock.Number()) {
					_, _, rewardV2List := rewardMap.ToList()
					governingV2Tx := governor.BuildStakingGoverningV2Tx(rewardV2List, uint32(conR.curEpoch), chainTag, bestNum)
					if governingV2Tx != nil {
						conR.logger.Info("Built governing V2 tx: ", "hash", governingV2Tx.ID().String(), "clauses-size", len(governingV2Tx.Clauses()))
						txs = append(txs, governingV2Tx)
					}
				} else {
					distList := rewardMap.GetDistList()
					// fmt.Println("**** Distribute List")
					// for _, d := range distList {
					// 	fmt.Println(d.String())
					// }
					// fmt.Println("-------------------------")

					governingTx := governor.BuildStakingGoverningTx(distList, uint32(conR.curEpoch), chainTag, bestNum)
					if governingTx != nil {
						conR.logger.Info("Built governing tx: ", "hash", governingTx.ID().String(), "clauses-size", len(governingTx.Clauses()))
						txs = append(txs, governingTx)
					}

					autobidList := rewardMap.GetAutobidList()
					// fmt.Println("**** Autobid List")
					// for _, a := range autobidList {
					// 	fmt.Println(a.String())
					// }
					// fmt.Println("-------------------------")

					autobidTxs := governor.BuildAutobidTxs(autobidList, chainTag, bestNum)
					if len(autobidTxs) > 0 {
						txs = append(txs, autobidTxs...)
						for _, tx := range autobidTxs {
							conR.logger.Info("Built autobid tx: ", "hash", tx.ID().String(), "clauses-size", len(tx.Clauses()))
						}
					}
				}
			} else {
				fmt.Println("-------------------------")
				fmt.Println("Reward Map is empty")
				fmt.Println("-------------------------")
			}
		}
	}

	if tx := governor.BuildAccountLockGoverningTx(chainTag, bestNum, curEpoch); tx != nil {
		txs = append(txs, tx)
		conR.logger.Info("Built account lock tx: ", "hash", tx.ID().String(), "clauses-size", len(tx.Clauses()))
	}
	conR.logger.Info("buildKBlockTxs", "size", len(txs))
	return txs
}