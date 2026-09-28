package governance

// takeSnapshot 在指定区块高度上展开委托链，生成不可变的投票权重快照。
// 语义：每个人的余额权重沿委托链向上汇总到最终受托人；只有最终受托人
// 能代表该权重投票（例如 A->B->C，则 A、B 的权重都计入 C）。
func takeSnapshot(blocks []Block, height int64) (*Snapshot, error) {
	if height < 0 || int(height) >= len(blocks) || blocks[height].Height != height {
		return nil, newError(CodeSnapshotUnavailable, "snapshot block height unavailable")
	}
	blk := blocks[height]
	weights := map[Address]int64{}
	for who, bal := range blk.Balances {
		if bal <= 0 {
			continue
		}
		root, err := delegationRoot(blk.Delegates, who)
		if err != nil {
			return nil, err
		}
		weights[root] += bal
	}
	total := int64(0)
	for _, w := range weights {
		total += w
	}
	return &Snapshot{BlockHeight: height, Weights: weights, TotalWeight: total}, nil
}

// delegationRoot 沿委托链找到最终受托人；数据损坏导致环时返回错误。
func delegationRoot(delegates map[Address]Address, who Address) (Address, error) {
	cur := who
	seen := map[Address]bool{who: true}
	for {
		next, ok := delegates[cur]
		if !ok || next == "" {
			return cur, nil
		}
		if seen[next] {
			return "", newError(CodeDelegationCycle, "cycle in delegation chain at "+string(next))
		}
		seen[next] = true
		cur = next
	}
}

// effectiveWeightOf 返回 who 在给定状态下的有效投票权重
// （who 作为最终受托人时可代表的全部余额）。
func effectiveWeightOf(balances map[Address]int64, delegates map[Address]Address, who Address) int64 {
	root, err := delegationRoot(delegates, who)
	if err != nil {
		return 0
	}
	if root != who {
		return 0
	}
	total := int64(0)
	for holder, bal := range balances {
		if bal <= 0 {
			continue
		}
		r, err := delegationRoot(delegates, holder)
		if err != nil {
			continue
		}
		if r == who {
			total += bal
		}
	}
	return total
}

// detectDelegationCycle 检测新增 from->to 委托后是否产生环。
func detectDelegationCycle(delegates map[Address]Address, from, to Address) bool {
	if from == to {
		return true
	}
	seen := map[Address]bool{from: true}
	cur := to
	for {
		if seen[cur] {
			return true
		}
		next, ok := delegates[cur]
		if !ok || next == "" {
			return false
		}
		seen[cur] = true
		cur = next
	}
}
