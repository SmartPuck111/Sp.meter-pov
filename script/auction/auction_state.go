// Copyright (c) 2020 The Meter.io developers

// Distributed under the GNU Lesser General Public License v3.0 software license, see the accompanying
// file LICENSE or <https://www.gnu.org/licenses/lgpl-3.0.html>

package auction

import (
	"bytes"
	"errors"
	"fmt"
	"math/big"
	"sort"

	"github.com/meterio/meter-pov/builtin"
	"github.com/meterio/meter-pov/meter"
	"github.com/meterio/meter-pov/runtime/statedb"
	setypes "github.com/meterio/meter-pov/script/types"
	"github.com/meterio/meter-pov/state"

	"github.com/ethereum/go-ethereum/common"
)

// ==================== account openation===========================
// from meter.ValidatorBenefitAddr ==> AuctionModuleAddr
func (a *Auction) TransferAutobidMTRToAuction(addr meter.Address, amount *big.Int, state *state.State, env *setypes.ScriptEnv) error {
	if amount.Sign() == 0 {
		return nil
	}

	meterBalance := state.GetEnergy(meter.ValidatorBenefitAddr)
	if meterBalance.Cmp(amount) < 0 {
		return fmt.Errorf("not enough meter balance in validator benefit address, balance:%v amount:%v", meterBalance, amount)
	}

	// a.logger.Info("transfer autobid MTR", "bidder", addr, "amount", amount)
	state.SubEnergy(meter.ValidatorBenefitAddr, amount)
	state.AddEnergy(meter.AuctionModuleAddr, amount)
	env.AddTransfer(meter.ValidatorBenefitAddr, meter.AuctionModuleAddr, amount, meter.MTR)
	return nil
}

// from addr == > AuctionModuleAddr
func (a *Auction) TransferMTRToAuction(addr meter.Address, amount *big.Int, state *state.State, env *setypes.ScriptEnv) error {
	if amount.Sign() == 0 {
		return nil
	}

	meterBalance := state.GetEnergy(addr)
	if meterBalance.Cmp(amount) < 0 {
		return errors.New("not enough meter")
	}

	a.logger.Info("transfer userbid MTR", "bidder", addr, "amount", amount)
	state.SubEnergy(addr, amount)
	state.AddEnergy(meter.AuctionModuleAddr, amount)
	env.AddTransfer(addr, meter.AuctionModuleAddr, amount, meter.MTR)
	return nil
}

func (a *Auction) SendMTRGToBidder(addr meter.Address, amount *big.Int, stateDB *statedb.StateDB, env *setypes.ScriptEnv) {
	if amount.Sign() == 0 {
		return
	}
	// in auction, MeterGov is mint action.
	stateDB.MintBalance(common.Address(addr), amount)
	env.AddTransfer(meter.ZeroAddress, addr, amount, meter.MTRG)
	return
}

// form AuctionModuleAddr ==> meter.ValidatorBenefitAddr
func (a *Auction) TransferMTRToValidatorBenefit(amount *big.Int, state *state.State, env *setypes.ScriptEnv) error {
	if amount.Sign() == 0 {
		return nil
	}

	meterBalance := state.GetEnergy(meter.AuctionModuleAddr)
	if meterBalance.Cmp(amount) < 0 {
		return errors.New("not enough meter")
	}

	state.SubEnergy(meter.AuctionModuleAddr, amount)
	state.AddEnergy(meter.ValidatorBenefitAddr, amount)
	env.AddTransfer(meter.AuctionModuleAddr, meter.ValidatorBenefitAddr, amount, meter.MTR)

	return nil
}

// //////////////////////
// called when auction is over
func (a *Auction) ClearAuction(cb *meter.AuctionCB, state *state.State, env *setypes.ScriptEnv) (*big.Int, *big.Int, []*meter.DistMtrg, error) {
	stateDB := statedb.New(state)
	ValidatorBenefitRatio := builtin.Params.Native(state).Get(meter.KeyValidatorBenefitRatio)

	actualPrice := new(big.Int).Mul(cb.RcvdMTR, big.NewInt(1e18))
	if cb.RlsdMTRG.Cmp(big.NewInt(0)) > 0 {
		actualPrice = actualPrice.Div(actualPrice, cb.RlsdMTRG)
	} else {
		actualPrice = cb.RsvdPrice
	}
	if actualPrice.Cmp(cb.RsvdPrice) < 0 {
		actualPrice = cb.RsvdPrice
	}

	blockNum := env.GetTxCtx().BlockRef.Number()
	total := big.NewInt(0)
	distMtrg := []*meter.DistMtrg{}
	if meter.IsTeslaFork3(blockNum) {

		groupTxMap := make(map[meter.Address]*big.Int)
		sortedAddresses := make([]meter.Address, 0)
		for _, tx := range cb.AuctionTxs {
			mtrg := new(big.Int).Mul(tx.Amount, big.NewInt(1e18))
			mtrg = new(big.Int).Div(mtrg, actualPrice)

			if _, ok := groupTxMap[tx.Address]; ok == true {
				groupTxMap[tx.Address] = new(big.Int).Add(groupTxMap[tx.Address], mtrg)
			} else {
				groupTxMap[tx.Address] = new(big.Int).Set(mtrg)
				sortedAddresses = append(sortedAddresses, tx.Address)
			}
		}

		sort.SliceStable(sortedAddresses, func(i, j int) bool {
			return bytes.Compare(sortedAddresses[i].Bytes(), sortedAddresses[j].Bytes()) <= 0
		})

		for _, addr := range sortedAddresses {
			mtrg := groupTxMap[addr]
			a.SendMTRGToBidder(addr, mtrg, stateDB, env)
			total = total.Add(total, mtrg)
			distMtrg = append(distMtrg, &meter.DistMtrg{Addr: addr, Amount: mtrg})
		}
	} else {
		for _, tx := range cb.AuctionTxs {
			mtrg := new(big.Int).Mul(tx.Amount, big.NewInt(1e18))
			mtrg = new(big.Int).Div(mtrg, actualPrice)

			a.SendMTRGToBidder(tx.Address, mtrg, stateDB, env)
			if (meter.IsMainNet() && blockNum < meter.TeslaFork3_MainnetAuctionDefectStartNum) || meter.IsTestNet() {
				total = total.Add(total, mtrg)
			}
			distMtrg = append(distMtrg, &meter.DistMtrg{Addr: tx.Address, Amount: mtrg})
		}

	}

	// sometimes accuracy cause negative value
	leftOver := new(big.Int).Sub(cb.RlsdMTRG, total)
	if leftOver.Sign() < 0 {
		leftOver = big.NewInt(0)
	}

	// send the remainings to accumulate accounts
	a.SendMTRGToBidder(meter.AuctionLeftOverAccount, cb.RsvdMTRG, stateDB, env)
	a.SendMTRGToBidder(meter.AuctionLeftOverAccount, leftOver, stateDB, env)

	// 40% of received meter to AuctionValidatorBenefitAddr
	amount := new(big.Int).Mul(cb.RcvdMTR, ValidatorBenefitRatio)
	amount = amount.Div(amount, big.NewInt(1e18))
	a.TransferMTRToValidatorBenefit(amount, state, env)

	a.logger.Info("finished auctionCB clear...", "actualPrice", actualPrice.String(), "leftOver", leftOver.String(), "validatorBenefit", amount.String())
	return actualPrice, leftOver, distMtrg, nil
}
