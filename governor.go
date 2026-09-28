package governance

import "time"

// Governor 是对外门面：持锁串行化所有状态变更，模拟链上状态机。
// 区块高度单调递增；状态经 Store 原子持久化，重启后可恢复。
//
// 所有变更方法都遵循同一节奏：加锁 -> 在内存状态上修改 -> Store.Save 原子落盘 -> 解锁。
// 因为先在内存完整变更再整体保存，落盘前的任何错误都不会产生半截状态；
// Save 是全量原子替换，因此崩溃/重启后只会看到变更前或变更后的完整状态。
type Governor struct {
	store Store
	mu    chan struct{}
	st    *State
}

// NewGovernor 从 store 加载状态；文件不存在则初始化空状态。
func NewGovernor(store Store) (*Governor, error) {
	st, err := store.Load()
	if err != nil {
		if err != ErrNotFound {
			return nil, err
		}
		st = newState()
		if err := store.Save(st); err != nil {
			return nil, err
		}
	}
	return &Governor{store: store, mu: make(chan struct{}, 1), st: st}, nil
}

func (g *Governor) lock()   { g.mu <- struct{}{} }
func (g *Governor) unlock() { <-g.mu }

// commit 原子持久化当前内存状态。
func (g *Governor) commit() error { return g.store.Save(g.st) }

// AdvanceTo 把链高度推进到 block（只进不退）。
func (g *Governor) AdvanceTo(block int64) error {
	g.lock()
	defer g.unlock()
	if block < g.st.CurrentBlock {
		return wrapf(ErrInvalidArgument, "cannot rewind block %d to %d", g.st.CurrentBlock, block)
	}
	if block == g.st.CurrentBlock {
		return nil
	}
	g.st.CurrentBlock = block
	return g.commit()
}

// Height 返回当前区块高度。
func (g *Governor) Height() int64 {
	g.lock()
	defer g.unlock()
	return g.st.CurrentBlock
}

// Mint 在当前区块给 addr 增加权重。
func (g *Governor) Mint(addr string, amount int64) error {
	g.lock()
	defer g.unlock()
	if err := g.st.Ledger.Mint(addr, amount, g.st.CurrentBlock); err != nil {
		return err
	}
	return g.commit()
}

// Burn 在当前区块销毁 addr 的权重。
func (g *Governor) Burn(addr string, amount int64) error {
	g.lock()
	defer g.unlock()
	if err := g.st.Ledger.Burn(addr, amount, g.st.CurrentBlock); err != nil {
		return err
	}
	return g.commit()
}

// Transfer 在当前区块转移权重。
func (g *Governor) Transfer(from, to string, amount int64) error {
	g.lock()
	defer g.unlock()
	if err := g.st.Ledger.Transfer(from, to, amount, g.st.CurrentBlock); err != nil {
		return err
	}
	return g.commit()
}

// Balance 查询某地址当前区块权重。
func (g *Governor) Balance(addr string) (int64, error) {
	g.lock()
	defer g.unlock()
	return g.st.Ledger.weightAt(addr, g.st.CurrentBlock), nil
}

// TotalSupply 查询当前区块权重总量。
func (g *Governor) TotalSupply() (int64, error) {
	g.lock()
	defer g.unlock()
	return g.st.Ledger.totalSupplyAt(g.st.CurrentBlock), nil
}

// Delegate 在当前区块设置委托。
func (g *Governor) Delegate(addr, delegatee string) error {
	g.lock()
	defer g.unlock()
	if err := g.st.Delegations.Delegate(addr, delegatee, g.st.CurrentBlock); err != nil {
		return err
	}
	return g.commit()
}

// Revoke 在当前区块撤销委托。
func (g *Governor) Revoke(addr string) error {
	g.lock()
	defer g.unlock()
	if err := g.st.Delegations.Revoke(addr, g.st.CurrentBlock); err != nil {
		return err
	}
	return g.commit()
}

// CreateProposal 在当前区块校验并创建提案（快照在当前区块物化）。
func (g *Governor) CreateProposal(in CreateProposalInput) (*Proposal, error) {
	g.lock()
	defer g.unlock()
	p, err := validateAndCreateProposal(g.st, in, time.Now().UTC())
	if err != nil {
		return nil, err
	}
	g.st.Proposals[p.ID] = p
	if err := g.commit(); err != nil {
		return nil, err
	}
	return p, nil
}

// Vote 在当前区块对提案投票。
func (g *Governor) Vote(proposalID, voter string, option VoteOption) (*Vote, error) {
	g.lock()
	defer g.unlock()
	v, err := castVote(g.st, proposalID, voter, option, g.st.CurrentBlock, time.Now().UTC())
	if err != nil {
		return nil, err
	}
	if err := g.commit(); err != nil {
		return nil, err
	}
	return v, nil
}

// Tally 在当前区块计票（要求投票期已结束）。
func (g *Governor) Tally(id string) (*TallyResult, error) {
	g.lock()
	defer g.unlock()
	r, err := tallyProposal(g.st, id, time.Now().UTC())
	if err != nil {
		return nil, err
	}
	if err := g.commit(); err != nil {
		return nil, err
	}
	return r, nil
}

// Execute 在当前区块执行计票成功的提案。
func (g *Governor) Execute(id string) (*ExecutionResult, error) {
	g.lock()
	defer g.unlock()
	r, err := executeProposal(g.st, id, g.st.CurrentBlock, time.Now().UTC())
	// 即便返回错误，失败/回滚结论也已写入状态，需要持久化以便重启后一致。
	if r != nil {
		_ = g.commit()
	}
	if err != nil {
		return r, err
	}
	if err := g.commit(); err != nil {
		return nil, err
	}
	return r, nil
}

// GetProposal 返回提案（拷贝）。
func (g *Governor) GetProposal(id string) (*Proposal, error) {
	g.lock()
	defer g.unlock()
	p, ok := g.st.Proposals[id]
	if !ok {
		return nil, wrapf(ErrNotFound, "proposal %s", id)
	}
	cp := *p
	return &cp, nil
}

// GetTally 返回已持久化的计票结论。
func (g *Governor) GetTally(id string) (*TallyResult, error) {
	g.lock()
	defer g.unlock()
	t, ok := g.st.Tallies[id]
	if !ok {
		return nil, wrapf(ErrNotFound, "tally for proposal %s", id)
	}
	cp := *t
	return &cp, nil
}

// GetExecution 返回已持久化的执行结果。
func (g *Governor) GetExecution(id string) (*ExecutionResult, error) {
	g.lock()
	defer g.unlock()
	e, ok := g.st.Executions[id]
	if !ok {
		return nil, wrapf(ErrNotFound, "execution for proposal %s", id)
	}
	cp := *e
	return &cp, nil
}

// GetVotes 返回某提案下全部选票，按投票者地址排序。
func (g *Governor) GetVotes(id string) ([]*Vote, error) {
	g.lock()
	defer g.unlock()
	ballots := g.st.Votes[id]
	out := make([]*Vote, 0, len(ballots))
	for _, v := range ballots {
		cp := *v
		out = append(out, &cp)
	}
	sortVotes(out)
	return out, nil
}

// SnapshotAt 在指定区块临时物化一份快照（只读诊断用，不写入状态）。
func (g *Governor) SnapshotAt(block int64) (*Snapshot, error) {
	g.lock()
	defer g.unlock()
	if block < 0 || block > g.st.CurrentBlock {
		return nil, wrapf(ErrInvalidArgument, "block %d out of range [0,%d]", block, g.st.CurrentBlock)
	}
	return materializeSnapshot(&g.st.Ledger, &g.st.Delegations, block)
}

// HasRole 查询角色成员关系。
func (g *Governor) HasRole(role, member string) (bool, error) {
	g.lock()
	defer g.unlock()
	members, ok := g.st.Roles[role]
	if !ok {
		return false, nil
	}
	return members[member], nil
}
