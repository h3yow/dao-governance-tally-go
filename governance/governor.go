package governance

import "sync"

// Governor 是本地 DAO 治理门面：所有状态变更都经过它，持有一把读写锁，
// 并在每次变更后立即持久化。重启后从状态文件恢复，结论保持一致。
type Governor struct {
	mu    sync.RWMutex
	store *Store
}

// NewGovernor 基于已打开的 Store 构建 Governor。
func NewGovernor(store *Store) *Governor {
	return &Governor{store: store}
}

func (g *Governor) Close() error {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.store.Close()
}

// Height 返回当前链高度（每个写操作 +1）。
func (g *Governor) Height() int64 {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return g.store.State().Height
}

// commitBlock 把当前余额/委托的深拷贝作为一个新区块追加并持久化。
// 调用方必须持有 g.mu 写锁，且已完成本次状态修改。
func (g *Governor) commitBlock() error {
	st := g.store.State()
	balances := make(map[Address]int64, len(st.Balances))
	for k, v := range st.Balances {
		balances[k] = v
	}
	delegates := make(map[Address]Address, len(st.Delegates))
	for k, v := range st.Delegates {
		delegates[k] = v
	}
	st.Height++
	st.Blocks = append(st.Blocks, Block{
		Height:    st.Height,
		Balances:  balances,
		Delegates: delegates,
	})
	return g.store.Save()
}

// cloneSnapshot 深拷贝快照，避免外部修改提案内部状态。
func cloneSnapshot(s Snapshot) Snapshot {
	w := make(map[Address]int64, len(s.Weights))
	for k, v := range s.Weights {
		w[k] = v
	}
	return Snapshot{BlockHeight: s.BlockHeight, Weights: w, TotalWeight: s.TotalWeight}
}
