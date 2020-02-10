package staking

import (
	b64 "encoding/base64"
	"errors"
	"fmt"
	"math/big"
	"net"
	"strings"

	crypto "github.com/ethereum/go-ethereum/crypto"

	"github.com/dfinlab/meter/meter"
	"github.com/dfinlab/meter/types"
)

var (
	// delegate minimum requirement 300 MTRG
	MIN_REQUIRED_BY_DELEGATE *big.Int = big.NewInt(0).Mul(big.NewInt(int64(300)), big.NewInt(int64(1e18)))
)

type Delegate struct {
	Address     meter.Address
	PubKey      []byte //ecdsa.PublicKey
	Name        []byte
	VotingPower *big.Int
	IPAddr      []byte
	Port        uint16
}

type DelegateList struct {
	delegates []*Delegate
}

func newDelegateList(delegates []*Delegate) *DelegateList {
	return &DelegateList{delegates: delegates}
}

func (d *Delegate) ToString() string {
	pubKeyEncoded := b64.StdEncoding.EncodeToString(d.PubKey)
	return fmt.Sprintf("Delegate(%v) Addr=%v PubKey=%v IP=%v:%v VotingPower=%d",
		string(d.Name), d.Address, pubKeyEncoded, string(d.IPAddr), d.Port, d.VotingPower.Uint64())
}

// match minimum requirements?
// 1. > 300 MTRG
func (d *Delegate) MinimumRequirements() bool {
	if d.VotingPower.Cmp(MIN_REQUIRED_BY_DELEGATE) < 0 {
		return false
	}
	return true
}

func (l *DelegateList) CleanAll() error {
	l.delegates = []*Delegate{}
	return nil
}

func (l *DelegateList) Members() string {
	members := make([]string, 0)
	for _, d := range l.delegates {
		members = append(members, string(d.Name))
	}
	return strings.Join(members, ", ")
}

func (l *DelegateList) SetDelegates(delegates []*Delegate) error {
	l.delegates = delegates
	return nil
}

func (l *DelegateList) GetDelegates() []*Delegate {
	return l.delegates
}

func (l *DelegateList) Add(c *Delegate) error {
	l.delegates = append(l.delegates, c)
	return nil
}

func (l *DelegateList) ToString() string {
	if l == nil || len(l.delegates) == 0 {
		return "DelegateList (size:0)"
	}
	s := []string{fmt.Sprintf("DelegateList (size:%v) {", len(l.delegates))}
	for k, v := range l.delegates {
		s = append(s, fmt.Sprintf("%v. %v", k, v.ToString()))
	}
	s = append(s, "}")
	return strings.Join(s, "\n")
}

//  api routine interface
func GetLatestDelegateList() (*DelegateList, error) {
	staking := GetStakingGlobInst()
	if staking == nil {
		log.Warn("staking is not initilized...")
		err := errors.New("staking is not initilized...")
		return nil, err
	}

	best := staking.chain.BestBlock()
	state, err := staking.stateCreator.NewState(best.Header().StateRoot())
	if err != nil {
		return nil, err
	}

	list := staking.GetDelegateList(state)
	// fmt.Println("delegateList from state", list.ToString())

	return list, nil
}

//  consensus routine interface
func GetInternalDelegateList() ([]*types.Delegate, error) {
	delegateList := []*types.Delegate{}
	staking := GetStakingGlobInst()
	if staking == nil {
		fmt.Println("staking is not initilized...")
		err := errors.New("staking is not initilized...")
		return delegateList, err
	}

	best := staking.chain.BestBlock()
	state, err := staking.stateCreator.NewState(best.Header().StateRoot())
	if err != nil {
		return delegateList, err
	}

	list := staking.GetDelegateList(state)
	// fmt.Println("delegateList from state\n", list.ToString())
	for _, s := range list.delegates {
		pubKeyBytes, err := b64.StdEncoding.DecodeString(string(s.PubKey))
		pubKey, err := crypto.UnmarshalPubkey(pubKeyBytes)
		if err != nil {
			fmt.Println("Unmarshal publicKey failed ...")
			continue
		}
		// delegates must satisfy the minimum requirements
		if ok := s.MinimumRequirements(); ok == false {
			continue
		}

		d := &types.Delegate{
			Name:        s.Name,
			Address:     s.Address,
			PubKey:      *pubKey,
			VotingPower: s.VotingPower.Div(s.VotingPower, big.NewInt(1e12)).Int64(),
			NetAddr: types.NetAddress{
				IP:   net.ParseIP(string(s.IPAddr)),
				Port: s.Port},
		}
		delegateList = append(delegateList, d)
	}
	return delegateList, nil
}
