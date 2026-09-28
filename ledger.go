package governance

import "sort"

// 权重账本：记录每个地址权重的全量历史点，可查询任意区块的快照值。
// 快照之后新增/转移/销毁的权重只会追加到新的历史点，
// 不会改写任何旧点 —— 因此旧区块的查询结果永远稳定。

func newLedgerState() LedgerState {
	return LedgerState{
		Balances:    map[string][]BalancePoint{},
		TotalSupply: nil,
	}
}

// valueAt 在一串按 block 升序排列的历史点中，
// 返回 <= block 的最后一个点的值；不存在则为 0。
func valueAt(points []BalancePoint, block int64) int64 {
	i := sort.Search(len(points), func(i int) bool {
		return points[i].Block > block
	})
	if i == 0 {
		return 0
	}
	return points[i-1].Weight
}

// appendPoint 追加历史点：同区块连续变更只保留最新值（同一区块内合并），
// 新区块必须不小于最后一个点的区块。
func appendPoint(points []BalancePoint, block, weight int64) []BalancePoint {
	if n := len(points); n > 0 && points[n-1].Block == block {
		points[n-1].Weight = weight
		return points
	}
	return append(points, BalancePoint{Block: block, Weight: weight})
}

// weightAt 返回 addr 在 block 时刻的权重。
func (l *LedgerState) weightAt(addr string, block int64) int64 {
	return valueAt(l.Balances[addr], block)
}

// totalSupplyAt 返回 block 时刻的权重总量。
func (l *LedgerState) totalSupplyAt(block int64) int64 {
	return valueAt(l.TotalSupply, block)
}

func checkPositiveAmount(amount int64) error {
	if amount <= 0 {
		return wrapf(ErrInvalidArgument, "amount must be positive, got %d", amount)
	}
	return nil
}

// Mint 给 addr 在 block 增加权重。
func (l *LedgerState) Mint(addr string, amount, block int64) error {
	if addr == "" {
		return wrapf(ErrInvalidArgument, "mint target address is empty")
	}
	if err := checkPositiveAmount(amount); err != nil {
		return err
	}
	newBal := l.weightAt(addr, block) + amount
	l.Balances[addr] = appendPoint(l.Balances[addr], block, newBal)
	l.TotalSupply = appendPoint(l.TotalSupply, block, l.totalSupplyAt(block)+amount)
	return nil
}

// Burn 销毁 addr 在 block 的权重，余额不足返回 ErrInsufficientBalance。
func (l *LedgerState) Burn(addr string, amount, block int64) error {
	if addr == "" {
		return wrapf(ErrInvalidArgument, "burn target address is empty")
	}
	if err := checkPositiveAmount(amount); err != nil {
		return err
	}
	cur := l.weightAt(addr, block)
	if cur < amount {
		return wrapf(ErrInsufficientBalance, "burn %d from %s whose balance is %d at block %d",
			amount, addr, cur, block)
	}
	newBal := cur - amount
	l.Balances[addr] = appendPoint(l.Balances[addr], block, newBal)
	l.TotalSupply = appendPoint(l.TotalSupply, block, l.totalSupplyAt(block)-amount)
	return nil
}

// Transfer 在 block 把权重从 from 转到 to。
func (l *LedgerState) Transfer(from, to string, amount, block int64) error {
	if from == "" || to == "" {
		return wrapf(ErrInvalidArgument, "transfer address is empty")
	}
	if from == to {
		return wrapf(ErrInvalidArgument, "cannot transfer to self: %s", from)
	}
	if err := checkPositiveAmount(amount); err != nil {
		return err
	}
	fromBal := l.weightAt(from, block)
	if fromBal < amount {
		return wrapf(ErrInsufficientBalance, "transfer %d from %s whose balance is %d at block %d",
			amount, from, fromBal, block)
	}
	toBal := l.weightAt(to, block)
	// 总量不变，只动两个地址的历史点。
	l.Balances[from] = appendPoint(l.Balances[from], block, fromBal-amount)
	l.Balances[to] = appendPoint(l.Balances[to], block, toBal+amount)
	return nil
}
