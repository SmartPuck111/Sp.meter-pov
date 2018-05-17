// Copyright (c) 2018 The VeChainThor developers

// Distributed under the GNU Lesser General Public License v3.0 software license, see the accompanying
// file LICENSE or <https://www.gnu.org/licenses/lgpl-3.0.html>

package statedb

import (
	"math/big"

	"github.com/vechain/thor/thor"
)

// State is defined to decouple with state.State.
type State interface {
	GetBalance(thor.Address) *big.Int
	GetCode(thor.Address) []byte
	GetCodeHash(thor.Address) thor.Bytes32
	GetStorage(thor.Address, thor.Bytes32) thor.Bytes32
	Exists(thor.Address) bool
	//	ForEachStorage(addr thor.Address, cb func(key thor.Bytes32, value []byte) bool)

	SetBalance(thor.Address, *big.Int)
	SetCode(thor.Address, []byte)
	SetStorage(thor.Address, thor.Bytes32, thor.Bytes32)
	Delete(thor.Address)

	NewCheckpoint() int
	RevertTo(int)
}
