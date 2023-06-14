package metertracker

import (
	"errors"
	"math/big"

	"github.com/meterio/meter-pov/meter"
)

var (
	errLessThanMinBoundBalance = errors.New("bound amount < minimal " + new(big.Int).Div(meter.MIN_BOUND_BALANCE, big.NewInt(1e18)).String() + " MTRG")
	errZeroAmount              = errors.New("zero amount")
	errEmptyCandidate          = errors.New("empty candidate address")
	errCandidateNotListed      = errors.New("candidate not listed")
	errNotEnoughBalance        = errors.New("not enough balance")
	errNotEnoughBoundedBalance = errors.New("not enough bounded balance")
	errSelfVoteNotAllowed      = errors.New("self vote not allowed")
	errNotEnoughVotes          = errors.New("not enough votes")

	errBucketNotListed                = errors.New("bucket not listed")
	errBucketNotOwned                 = errors.New("bucket not owned")
	errNoUpdateAllowedOnForeverBucket = errors.New("no update allowed on forever bucket")
	errBucketAlreadyUnbounded         = errors.New("bucket already unbounded")
	errBucketNotEnoughValue           = errors.New("not enough value")
)

func (e *MeterTracker) BoundMeterGov(addr meter.Address, amount *big.Int) error {
	if amount.Sign() == 0 {
		return nil
	}
	state := e.state
	meterGov := state.GetBalance(addr)
	meterGovBounded := state.GetBoundedBalance(addr)

	// meterGov should >= amount
	if meterGov.Cmp(amount) == -1 {
		return errNotEnoughBalance
	}

	state.SetBalance(addr, new(big.Int).Sub(meterGov, amount))
	state.SetBoundedBalance(addr, new(big.Int).Add(meterGovBounded, amount))

	return nil
}

func (e *MeterTracker) UnboundMeterGov(addr meter.Address, amount *big.Int) error {
	if amount.Sign() == 0 {
		return nil
	}
	state := e.state
	meterGov := state.GetBalance(addr)
	meterGovBounded := state.GetBoundedBalance(addr)

	// meterGov should >= amount
	if meterGov.Cmp(amount) == -1 {
		return errNotEnoughBalance
	}

	state.SetBalance(addr, new(big.Int).Sub(meterGov, amount))
	state.SetBoundedBalance(addr, new(big.Int).Add(meterGovBounded, amount))

	return nil
}

// create a bucket
func (e *MeterTracker) BucketOpen(owner meter.Address, candAddr meter.Address, amount *big.Int, ts uint64, nonce uint64) (bucketID meter.Bytes32, err error) {
	emptyBucketID := meter.Bytes32{}
	// assert amount not 0
	if amount.Sign() == 0 {
		return emptyBucketID, errZeroAmount
	}

	// assert amount should meet the stake minmial requirement
	if amount.Cmp(meter.MIN_BOUND_BALANCE) < 0 {
		return emptyBucketID, errLessThanMinBoundBalance
	}

	// assert candidate not empty
	if candAddr.IsZero() {
		return emptyBucketID, errEmptyCandidate
	}

	// assert balance(owner) > amount
	if e.state.GetBalance(owner).Cmp(amount) < 0 {
		return emptyBucketID, errNotEnoughBalance
	}

	// assert not self vote
	if owner.EqualFold(&candAddr) {
		return emptyBucketID, errSelfVoteNotAllowed
	}

	candidateList := e.state.GetCandidateList()
	bucketList := e.state.GetBucketList()

	// for existing bucket, convert this request into a bucket deposit
	for _, bkt := range bucketList.Buckets {
		if candAddr.EqualFold(&bkt.Candidate) && owner.EqualFold(&bkt.Owner) {
			return bkt.ID(), e.BucketDeposit(owner, bkt.ID(), amount)
		}
	}

	candidate := candidateList.Get(candAddr)
	if candidate == nil {
		return emptyBucketID, errCandidateNotListed
	}

	meterGov := e.state.GetBalance(owner)
	meterGovBounded := e.state.GetBoundedBalance(owner)

	e.state.SetBalance(owner, new(big.Int).Sub(meterGov, amount))
	e.state.SetBoundedBalance(owner, new(big.Int).Add(meterGovBounded, amount))

	newBucket := meter.NewBucket(owner, candAddr, amount, meter.MTRG, meter.ONE_WEEK_LOCK, meter.ONE_WEEK_LOCK_RATE, 100 /*autobid*/, ts, nonce)
	bucketList.Add(newBucket)
	candidate.AddBucket(newBucket)

	e.state.SetCandidateList(candidateList)
	e.state.SetBucketList(bucketList)

	return newBucket.ID(), nil
}

func (e *MeterTracker) BucketClose(owner meter.Address, id meter.Bytes32, timestamp uint64) error {
	bucketList := e.state.GetBucketList()
	b := bucketList.Get(id)
	if err := e.checkBucket(b, owner); err != nil {
		return err
	}

	// sanity check done, take action
	b.Unbounded = true
	b.MatureTime = timestamp + meter.GetBoundLocktime(b.Option) // lock time

	e.state.SetBucketList(bucketList)
	return nil
}

func (e *MeterTracker) checkBucket(b *meter.Bucket, owner meter.Address) error {
	// assert bucket listed
	if b == nil {
		return errBucketNotListed
	}

	// assert bucket not unbounded
	if b.Unbounded {
		return errBucketAlreadyUnbounded
	}

	// assert bucket owned
	if b.Owner != owner {
		return errBucketNotOwned
	}

	// assert bucket not forever
	if b.IsForeverLock() {
		return errNoUpdateAllowedOnForeverBucket
	}
	return nil
}

func (e *MeterTracker) BucketDeposit(owner meter.Address, id meter.Bytes32, amount *big.Int) error {
	candidateList := e.state.GetCandidateList()
	bucketList := e.state.GetBucketList()

	b := bucketList.Get(id)
	if err := e.checkBucket(b, owner); err != nil {
		return err
	}

	// assert balance(owner) > amount
	if e.state.GetBalance(owner).Cmp(amount) < 0 {
		return errNotEnoughBalance
	}

	// bound account balance
	err := e.BoundMeterGov(owner, amount)
	if err != nil {
		return err
	}

	// NOTICE: no bonus is calculated, since it will be updated automatically during governing

	// update bucket values
	b.BonusVotes = 0
	b.Value.Add(b.Value, amount)
	b.TotalVotes.Add(b.TotalVotes, amount)

	// update candidate totalVotes with deposited amount
	if !b.Candidate.IsZero() {
		if cand := candidateList.Get(b.Candidate); cand != nil {
			cand.TotalVotes.Add(cand.TotalVotes, amount)
		}
	}

	e.state.SetBucketList(bucketList)
	e.state.SetCandidateList(candidateList)
	return nil
}

func (e *MeterTracker) BucketWithdraw(owner meter.Address, id meter.Bytes32, amount *big.Int, recipient meter.Address, ts uint64, nonce uint64) (meter.Bytes32, error) {
	candidateList := e.state.GetCandidateList()
	bucketList := e.state.GetBucketList()

	emptyBktID := meter.Bytes32{}
	b := bucketList.Get(id)

	if err := e.checkBucket(b, owner); err != nil {
		return emptyBktID, err
	}

	// assert boundedBalance(owner) > amount
	if e.state.GetBoundedBalance(owner).Cmp(amount) < 0 {
		return emptyBktID, errNotEnoughBoundedBalance
	}

	// assert bucket value > amount
	if b.Value.Cmp(amount) < 0 || b.TotalVotes.Cmp(amount) < 0 {
		return emptyBktID, errBucketNotEnoughValue
	}

	// assert leftover votes > staking requirement
	valueAfterWithdraw := new(big.Int).Sub(b.Value, amount)
	if valueAfterWithdraw.Cmp(meter.MIN_BOUND_BALANCE) < 0 {
		return emptyBktID, errLessThanMinBoundBalance
	}

	// bonus is substracted porpotionally
	oldBonus := new(big.Int).Sub(b.TotalVotes, b.Value)
	// bonus delta = oldBonus * (amount/bucket value)
	bonusDelta := new(big.Int).Mul(oldBonus, amount)
	bonusDelta.Div(bonusDelta, b.Value)

	// update old bucket
	b.BonusVotes = 0
	b.Value.Sub(b.Value, amount)
	b.TotalVotes.Sub(b.TotalVotes, amount)
	b.TotalVotes.Sub(b.TotalVotes, bonusDelta)

	// transfer bounded balance
	ownerBounded := e.state.GetBoundedBalance(owner)
	e.state.SetBoundedBalance(owner, new(big.Int).Sub(ownerBounded, amount))
	recipientBounded := e.state.GetBoundedBalance(recipient)
	e.state.SetBoundedBalance(recipient, new(big.Int).Add(recipientBounded, amount))

	// create unbounded new bucket
	newBucket := meter.NewBucket(recipient, b.Candidate, amount, uint8(b.Token), meter.ONE_WEEK_LOCK, b.Rate, b.Autobid, ts, nonce)
	newBucket.Unbounded = true
	newBucket.MatureTime = ts + meter.GetBoundLocktime(newBucket.Option) // lock time
	newBucketID := newBucket.BucketID

	cand := candidateList.Get(b.Candidate)
	if cand != nil {
		cand.TotalVotes.Sub(cand.TotalVotes, bonusDelta)
		cand.Buckets = append(cand.Buckets, newBucketID)
	}
	// update bucket list with new bucket
	bucketList.Add(newBucket)

	e.state.SetBucketList(bucketList)
	e.state.SetCandidateList(candidateList)
	return newBucketID, nil
}

func (e *MeterTracker) BucketUpdateCandidate(owner meter.Address, id meter.Bytes32, newCandidateAddr meter.Address) error {
	candidateList := e.state.GetCandidateList()
	bucketList := e.state.GetBucketList()

	b := bucketList.Get(id)
	// assert bucket listed
	if b == nil {
		return errBucketNotListed
	}

	// assert bucket owned
	if b.Owner != owner {
		return errBucketNotOwned
	}

	// assert bucket not forever
	if b.IsForeverLock() {
		return errNoUpdateAllowedOnForeverBucket
	}

	// assert candidate listed
	nc := candidateList.Get(newCandidateAddr)
	if nc == nil {
		return errCandidateNotListed
	}

	c := candidateList.Get(b.Candidate)
	// subtract totalVotes from old candidate
	if c != nil {
		if c.TotalVotes.Cmp(b.TotalVotes) < 0 {
			return errNotEnoughVotes
		}
		// c.TotalVotes.Sub(c.TotalVotes, b.TotalVotes)
		c.RemoveBucket(b)
	}
	// add totalVotes to new candidate
	// nc.TotalVotes.Add(nc.TotalVotes, b.TotalVotes)
	nc.AddBucket(b)
	b.Candidate = nc.Addr

	bucketList.Remove(id)
	e.state.SetBucketList(bucketList)
	e.state.SetCandidateList(candidateList)
	return nil
}

func (e *MeterTracker) BucketMerge(owner meter.Address, fromBucketID meter.Bytes32, toBucketID meter.Bytes32) error {
	candidateList := e.state.GetCandidateList()
	bucketList := e.state.GetBucketList()

	fromBkt := bucketList.Get(fromBucketID)
	toBkt := bucketList.Get(toBucketID)
	if err := e.checkBucket(fromBkt, owner); err != nil {
		return err
	}
	if err := e.checkBucket(toBkt, owner); err != nil {
		return err
	}

	fromCand := candidateList.Get(fromBkt.Candidate)
	toCand := candidateList.Get(toBkt.Candidate)

	if fromCand != nil {
		fromCand.RemoveBucket(fromBkt)
	}
	toBkt.BonusVotes = toBkt.BonusVotes + fromBkt.BonusVotes
	toBkt.Value.Add(toBkt.Value, fromBkt.Value)
	toBkt.TotalVotes.Add(toBkt.TotalVotes, fromBkt.TotalVotes)

	if toCand != nil {
		toCand.TotalVotes.Add(toCand.TotalVotes, fromBkt.TotalVotes)
	}

	bucketList.Remove(fromBucketID)
	e.state.SetBucketList(bucketList)
	e.state.SetCandidateList(candidateList)
	return nil
}

func (e *MeterTracker) BucketTransferFund(owner meter.Address, fromBucketID meter.Bytes32, toBucketID meter.Bytes32, amount *big.Int) error {
	candidateList := e.state.GetCandidateList()
	bucketList := e.state.GetBucketList()

	fromBkt := bucketList.Get(fromBucketID)
	toBkt := bucketList.Get(toBucketID)
	if err := e.checkBucket(fromBkt, owner); err != nil {
		return err
	}
	if err := e.checkBucket(toBkt, owner); err != nil {
		return err
	}

	// assert boundedBalance(owner) > amount
	if e.state.GetBoundedBalance(owner).Cmp(amount) < 0 {
		return errNotEnoughBoundedBalance
	}

	// assert from bucket value > amount
	if fromBkt.Value.Cmp(amount) < 0 || fromBkt.TotalVotes.Cmp(amount) < 0 {
		return errBucketNotEnoughValue
	}

	// assert leftover votes > staking requirement
	valueAfterTransfer := new(big.Int).Sub(fromBkt.Value, amount)
	if valueAfterTransfer.Cmp(meter.MIN_BOUND_BALANCE) < 0 {
		return errLessThanMinBoundBalance
	}

	// bonus is substracted porpotionally
	fromBonus := new(big.Int).Sub(fromBkt.TotalVotes, fromBkt.Value)
	// bonus delta = oldBonus * (amount/bucket value)
	bonusDelta := new(big.Int).Mul(fromBonus, amount)
	bonusDelta.Div(bonusDelta, fromBkt.Value)

	// update from bucket
	fromBkt.BonusVotes = new(big.Int).Sub(fromBonus, bonusDelta).Uint64()
	fromBkt.Value.Sub(fromBkt.Value, amount)
	fromBkt.TotalVotes.Sub(fromBkt.TotalVotes, amount)
	fromBkt.TotalVotes.Sub(fromBkt.TotalVotes, bonusDelta)

	// update to bucket
	toBkt.BonusVotes = toBkt.BonusVotes + bonusDelta.Uint64()
	toBkt.Value.Add(toBkt.Value, amount)
	toBkt.TotalVotes.Add(toBkt.TotalVotes, amount)
	toBkt.TotalVotes.Add(toBkt.TotalVotes, bonusDelta)

	fromCand := candidateList.Get(fromBkt.Candidate)
	toCand := candidateList.Get(toBkt.Candidate)

	// update from candidate if exists
	if fromCand != nil {
		fromCand.TotalVotes.Sub(fromCand.TotalVotes, amount)
		fromCand.TotalVotes.Sub(fromCand.TotalVotes, bonusDelta)
	}

	// update to candidate if exists
	if toCand != nil {
		toCand.TotalVotes.Add(toCand.TotalVotes, amount)
		toCand.TotalVotes.Add(toCand.TotalVotes, bonusDelta)
	}

	e.state.SetBucketList(bucketList)
	e.state.SetCandidateList(candidateList)
	return nil
}

func (e *MeterTracker) BucketValue(id meter.Bytes32) (*big.Int, error) {
	bucketList := e.state.GetBucketList()
	b := bucketList.Get(id)
	if b == nil {
		return new(big.Int), errBucketNotListed
	}
	return b.Value, nil
}
