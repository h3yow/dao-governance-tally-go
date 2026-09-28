package governance

// Mint 铸造权重（模拟区块奖励等），产生一个新区块。
func (g *Governor) Mint(to Address, amount int64) error {
	if to == "" || amount <= 0 {
		return newError(CodeInvalidArgument, "mint requires non-empty address and positive amount")
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	st := g.store.State()
	st.Balances[to] += amount
	return g.commitBlock()
}

// Transfer 转移权重，产生一个新区块。
func (g *Governor) Transfer(from, to Address, amount int64) error {
	if from == "" || to == "" || amount <= 0 {
		return newError(CodeInvalidArgument, "transfer requires valid addresses and positive amount")
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	st := g.store.State()
	if st.Balances[from] < amount {
		return newError(CodeInsufficientBalance, "balance too low for transfer")
	}
	st.Balances[from] -= amount
	st.Balances[to] += amount
	return g.commitBlock()
}

// Burn 销毁权重，产生一个新区块。
func (g *Governor) Burn(from Address, amount int64) error {
	if from == "" || amount <= 0 {
		return newError(CodeInvalidArgument, "burn requires non-empty address and positive amount")
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	st := g.store.State()
	if st.Balances[from] < amount {
		return newError(CodeInsufficientBalance, "balance too low for burn")
	}
	st.Balances[from] -= amount
	return g.commitBlock()
}

// Delegate 建立 from -> to 的投票权委托。
// 自委托和任何会形成环的委托都会被拒绝（错误码可区分）。
// 委托只影响之后新建提案的快照，不改变历史快照。
func (g *Governor) Delegate(from, to Address) error {
	if from == "" || to == "" {
		return newError(CodeInvalidArgument, "delegation requires valid addresses")
	}
	if from == to {
		return newError(CodeSelfDelegation, "self delegation is not allowed")
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	st := g.store.State()
	if detectDelegationCycle(st.Delegates, from, to) {
		return newError(CodeDelegationCycle, "delegation would create a cycle")
	}
	st.Delegates[from] = to
	return g.commitBlock()
}

// Undelegate 撤销 from 当前的委托边。撤销不影响已经生成的历史快照。
func (g *Governor) Undelegate(from Address) error {
	if from == "" {
		return newError(CodeInvalidArgument, "undelegate requires non-empty address")
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	st := g.store.State()
	if _, ok := st.Delegates[from]; !ok {
		return newError(CodeNotFound, "no delegation to revoke")
	}
	delete(st.Delegates, from)
	return g.commitBlock()
}

// Balance 返回当前原始余额（不含委托展开）。
func (g *Governor) Balance(who Address) int64 {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return g.store.State().Balances[who]
}

// DelegateOf 返回 from 当前的直接受托人。
func (g *Governor) DelegateOf(who Address) (Address, bool) {
	g.mu.RLock()
	defer g.mu.RUnlock()
	to, ok := g.store.State().Delegates[who]
	return to, ok
}
