package governance

import "sort"

// 委托：地址可把自身权重在某个区块委托给另一个地址；
// 委托历史 append-only，撤销与新委托都只是追加新点。
// 因此旧区块的委托关系与据此物化的快照都不会被后续操作改写。

func newDelegationState() DelegationState {
	return DelegationState{Entries: map[string][]DelegationPoint{}}
}

func appendDelegation(entries []DelegationPoint, block int64, delegatee string) []DelegationPoint {
	if n := len(entries); n > 0 && entries[n-1].Block == block {
		entries[n-1].Delegatee = delegatee
		return entries
	}
	return append(entries, DelegationPoint{Block: block, Delegatee: delegatee})
}

// delegateeAt 返回 addr 在 block 时刻委托的目标；无委托返回空串。
func (d *DelegationState) delegateeAt(addr string, block int64) string {
	entries := d.Entries[addr]
	i := sort.Search(len(entries), func(i int) bool {
		return entries[i].Block > block
	})
	if i == 0 {
		return ""
	}
	return entries[i-1].Delegatee
}

// resolveDelegate 返回 addr 在 block 的最终被委托人：
// 沿委托链前进到尽头。环检测由 wouldCycle 保证不会发生，
// 这里仍用 visited 做防御性保护，避免异常历史导致死循环。
func (d *DelegationState) resolveDelegate(addr string, block int64) string {
	visited := map[string]bool{addr: true}
	cur := addr
	for {
		next := d.delegateeAt(cur, block)
		if next == "" || visited[next] {
			return cur
		}
		visited[next] = true
		cur = next
	}
}

// wouldCycle 判断在 block 新增 addr -> delegatee 的委托是否会形成环。
func (d *DelegationState) wouldCycle(addr, delegatee string, block int64) bool {
	visited := map[string]bool{addr: true}
	cur := delegatee
	for cur != "" {
		if visited[cur] {
			return true
		}
		visited[cur] = true
		cur = d.delegateeAt(cur, block)
	}
	return false
}

// Delegate 在 block 设置 addr -> delegatee 的委托。
// 不允许自委托；若形成环返回 ErrDelegationCycle。
func (d *DelegationState) Delegate(addr, delegatee string, block int64) error {
	if addr == "" || delegatee == "" {
		return wrapf(ErrInvalidArgument, "delegator and delegatee must be non-empty")
	}
	if addr == delegatee {
		return wrapf(ErrSelfDelegation, "%s cannot delegate to itself", addr)
	}
	if d.wouldCycle(addr, delegatee, block) {
		return wrapf(ErrDelegationCycle, "%s -> %s closes a delegation cycle at block %d",
			addr, delegatee, block)
	}
	d.Entries[addr] = appendDelegation(d.Entries[addr], block, delegatee)
	return nil
}

// Revoke 在 block 撤销 addr 的当前委托，无委托时返回 ErrDelegationNotFound。
func (d *DelegationState) Revoke(addr string, block int64) error {
	if addr == "" {
		return wrapf(ErrInvalidArgument, "delegator address is empty")
	}
	if d.delegateeAt(addr, block) == "" {
		return wrapf(ErrDelegationNotFound, "address %s has no active delegation at block %d", addr, block)
	}
	d.Entries[addr] = appendDelegation(d.Entries[addr], block, "")
	return nil
}

// effectivePower 计算某地址在 block 的有效投票权重。
// 语义：权重随委托关系归集到最终被委托人；
// 自己把权重委托出去的地址有效权重为 0（不能重复使用，避免一票多投）。
func effectivePowerAt(l *LedgerState, d *DelegationState, addr string, block int64) int64 {
	if d.delegateeAt(addr, block) != "" {
		return 0
	}
	power := l.weightAt(addr, block)
	// 把所有（传递）委托给 addr 的地址的权重累加进来。
	for other := range l.Balances {
		if other == addr {
			continue
		}
		if d.delegateeAt(other, block) != "" && d.resolveDelegate(other, block) == addr {
			power += l.weightAt(other, block)
		}
	}
	return power
}

// materializeSnapshot 在指定区块物化投票快照：
// 遍历所有在该区块有正权重的地址，把权重聚合到最终被委托人名下。
// 结果是一份独立拷贝，之后权重或委托再变化都与它无关。
func materializeSnapshot(l *LedgerState, d *DelegationState, block int64) (*Snapshot, error) {
	powers := map[string]int64{}
	addrs := make([]string, 0, len(l.Balances))
	for addr := range l.Balances {
		addrs = append(addrs, addr)
	}
	sort.Strings(addrs) // 确定性遍历，便于复现与解释
	for _, addr := range addrs {
		raw := l.weightAt(addr, block)
		if raw <= 0 {
			continue
		}
		rep := d.resolveDelegate(addr, block)
		// 若 addr 主动委托给了别人（最终代表 != 自己），权重归到代表名下。
		powers[rep] += raw
	}
	var total int64
	for _, w := range powers {
		total += w
	}
	snap := &Snapshot{Block: block, TotalWeight: total, Powers: powers}
	return snap, nil
}
