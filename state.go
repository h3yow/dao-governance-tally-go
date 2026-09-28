package governance

// newState 构造一个所有 map 均已初始化的空状态。
func newState() *State {
	return &State{
		Version:      1,
		CurrentBlock: 0,
		Ledger:       newLedgerState(),
		Delegations:  newDelegationState(),
		Roles:        map[string]map[string]bool{},
		Proposals:    map[string]*Proposal{},
		Votes:        map[string]map[string]*Vote{},
		Tallies:      map[string]*TallyResult{},
		Executions:   map[string]*ExecutionResult{},
	}
}

// normalize 在反序列化后补齐可能为 nil 的 map，保证后续代码可直接写入。
func (s *State) normalize() error {
	if s.Version == 0 {
		s.Version = 1
	}
	if s.Ledger.Balances == nil {
		s.Ledger.Balances = map[string][]BalancePoint{}
	}
	if s.Delegations.Entries == nil {
		s.Delegations.Entries = map[string][]DelegationPoint{}
	}
	if s.Roles == nil {
		s.Roles = map[string]map[string]bool{}
	}
	if s.Proposals == nil {
		s.Proposals = map[string]*Proposal{}
	}
	if s.Votes == nil {
		s.Votes = map[string]map[string]*Vote{}
	}
	if s.Tallies == nil {
		s.Tallies = map[string]*TallyResult{}
	}
	if s.Executions == nil {
		s.Executions = map[string]*ExecutionResult{}
	}
	for id, p := range s.Proposals {
		if p == nil {
			return wrapf(ErrInvalidArgument, "stored proposal %s is null", id)
		}
		if p.Snapshot != nil && p.Snapshot.Powers == nil {
			p.Snapshot.Powers = map[string]int64{}
		}
	}
	return nil
}
