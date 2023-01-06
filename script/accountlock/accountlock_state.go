// Copyright (c) 2020 The Meter.io developers

// Distributed under the GNU Lesser General Public License v3.0 software license, see the accompanying
// file LICENSE or <https://www.gnu.org/licenses/lgpl-3.0.html>

package accountlock

import (
	"bytes"
	"strings"

	"github.com/ethereum/go-ethereum/rlp"
	"github.com/meterio/meter-pov/meter"
	"github.com/meterio/meter-pov/state"
)

// Profile List
func (a *AccountLock) GetProfileList(state *state.State) (result *meter.ProfileList) {
	state.DecodeStorage(AccountLockAddr, AccountLockProfileKey, func(raw []byte) error {
		profiles := make([]*meter.Profile, 0)

		if len(strings.TrimSpace(string(raw))) >= 0 {
			err := rlp.Decode(bytes.NewReader(raw), &profiles)
			if err != nil {
				if err.Error() == "EOF" && len(raw) == 0 {
					// EOF is caused by no value, is not error case, so returns with empty slice
				} else {
					log.Warn("Error during decoding profile list", "err", err)
					return err
				}
			}
		}

		result = meter.NewProfileList(profiles)
		return nil
	})
	return
}

func (a *AccountLock) SetProfileList(lockList *meter.ProfileList, state *state.State) {
	/*****
	sort.SliceStable(lockList.Profiles, func(i, j int) bool {
		return bytes.Compare(lockList.Profiles[i].Addr.Bytes(), lockList.Profiles[j].Addr.Bytes()) <= 0
	})
	*****/
	state.EncodeStorage(AccountLockAddr, AccountLockProfileKey, func() ([]byte, error) {
		return rlp.EncodeToBytes(lockList.Profiles)
	})
}
