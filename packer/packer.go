// Copyright (c) 2020 The Meter.io developers

// Distributed under the GNU Lesser General Public License v3.0 software license, see the accompanying
// file LICENSE or <https://www.gnu.org/licenses/lgpl-3.0.html>

package packer

import (
	"github.com/meterio/meter-pov/block"
	"github.com/meterio/meter-pov/chain"
	"github.com/meterio/meter-pov/meter"
	"github.com/meterio/meter-pov/runtime"
	"github.com/meterio/meter-pov/state"
	"github.com/meterio/meter-pov/xenv"
	"github.com/pkg/errors"
)

// Packer to pack txs and build new blocks.
type Packer struct {
	chain          *chain.Chain
	stateCreator   *state.Creator
	nodeMaster     meter.Address
	beneficiary    *meter.Address
	targetGasLimit uint64
}

// New create a new Packer instance.
// The beneficiary is optional, it defaults to endorsor if not set.
func New(
	chain *chain.Chain,
	stateCreator *state.Creator,
	nodeMaster meter.Address,
	beneficiary *meter.Address) *Packer {

	return &Packer{
		chain,
		stateCreator,
		nodeMaster,
		beneficiary,
		0,
	}
}

// Mock create a packing flow upon given parent, but with a designated timestamp.
func (p *Packer) Mock(parent *block.Header, targetTime uint64, gasLimit uint64, candAddr *meter.Address) (*Flow, error) {
	state, err := p.stateCreator.NewState(parent.StateRoot())
	if err != nil {
		return nil, errors.Wrap(err, "state")
	}

	// if beneficiary is not set, set as candidate Address, so it is not confusing
	var beneficiary *meter.Address
	if p.beneficiary != nil {
		beneficiary = p.beneficiary
	} else {
		beneficiary = candAddr
	}

	rt := runtime.New(
		p.chain.NewSeeker(parent.ID()),
		state,
		&xenv.BlockContext{
			Beneficiary: *beneficiary,
			Signer:      p.nodeMaster,
			Number:      parent.Number() + 1,
			Time:        targetTime,
			GasLimit:    gasLimit,
			TotalScore:  parent.TotalScore() + 1,
		})

	return newFlow(p, parent, rt), nil
}

func (p *Packer) GasLimit(parentGasLimit uint64) uint64 {
	if p.targetGasLimit != 0 {
		return block.GasLimit(p.targetGasLimit).Qualify(parentGasLimit)
	}
	return parentGasLimit
}

// SetTargetGasLimit set target gas limit, the Packer will adjust block gas limit close to
// it as it can.
func (p *Packer) SetTargetGasLimit(gl uint64) {
	p.targetGasLimit = gl
}
