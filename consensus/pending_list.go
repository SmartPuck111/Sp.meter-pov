package consensus

type PendingList struct {
	messages map[uint32]consensusMsgInfo
	lowest   uint32
}

func NewPendingList() *PendingList {
	return &PendingList{
		messages: make(map[uint32]consensusMsgInfo),
		lowest:   0,
	}
}

func (p *PendingList) Add(mi *consensusMsgInfo) {
	var height uint32 // Query height
	switch msg := mi.Msg.(type) {
	case *PMProposalMessage:
		height = msg.ParentHeight
	case *PMNewViewMessage:
		height = msg.QCHeight
	default:
		return
	}
	if height < p.lowest {
		p.lowest = height
	}
	p.messages[height] = *mi
}

func (p *PendingList) GetLowestHeight() uint32 {
	return p.lowest
}

func (p *PendingList) CleanUpTo(height uint32) error {
	if height < p.lowest {
		return nil
	}

	for key, _ := range p.messages {
		if key <= height {
			delete(p.messages, key)
		}
	}
	p.lowest = height
	return nil
}

// clean all the pending messages
func (p *PendingList) CleanAll() error {
	for key := range p.messages {
		delete(p.messages, key)
	}
	p.lowest = 0
	return nil
}
