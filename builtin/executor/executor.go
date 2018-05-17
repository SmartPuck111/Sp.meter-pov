// Copyright (c) 2018 The VeChainThor developers

// Distributed under the GNU Lesser General Public License v3.0 software license, see the accompanying
// file LICENSE or <https://www.gnu.org/licenses/lgpl-3.0.html>

package executor

import (
	"github.com/vechain/thor/state"
	"github.com/vechain/thor/thor"
)

type Executor struct {
	addr  thor.Address
	state *state.State
}

func New(addr thor.Address, state *state.State) *Executor {
	return &Executor{addr, state}
}
