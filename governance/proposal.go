package governance

import (
	"fmt"
	"time"
)

// CreateProposalConfig 创建提案参数。
type CreateProposalConfig struct {
	Title            string
	Actions          []Action
	Quorum           float64 // 为空（<=0）时使用系统默认值
	Threshold        float64
	VoteWindowBlocks int64 // 投票窗口长度（区块数），必须为正
}

// CreateProposal 在当前链高度创建提案，并立即冻结该高度的投票权重快照。
func (g *Governor) CreateProposal(creator Address, cfg CreateProposalConfig) (*Proposal, error) {
	if creator == "" {
		return nil, newError(CodeInvalidArgument, "creator required")
	}
	if cfg.VoteWindowBlocks <= 0 {
		return nil, newError(CodeInvalidArgument, "vote window must be positive blocks")
	}
	if cfg.Quorum < 0 || cfg.Quorum > 1 || cfg.Threshold < 0 || cfg.Threshold > 1 {
		return nil, newError(CodeInvalidArgument, "quorum and threshold must be within [0,1]")
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	st := g.store.State()
	if cfg.Quorum <= 0 {
		cfg.Quorum = st.DefaultQuorum
	}
	if cfg.Threshold <= 0 {
		cfg.Threshold = st.DefaultThreshold
	}
	snap, err := takeSnapshot(st.Blocks, st.Height)
	if err != nil {
		return nil, err
	}
	st.NextProposalSeq++
	id := nextProposalID(st.NextProposalSeq, time.Now().UTC())
	p := &Proposal{
		ID:            id,
		Title:         cfg.Title,
		Creator:       creator,
		CreatedHeight: st.Height,
		Snapshot:      *snap,
		StartHeight:   st.Height,
		EndHeight:     st.Height + cfg.VoteWindowBlocks,
		Quorum:        cfg.Quorum,
		Threshold:     cfg.Threshold,
		Actions:       append([]Action(nil), cfg.Actions...),
		Status:        StatusActive,
		Votes:         map[Address]VoteChoice{},
		VoteHeights:   map[Address]int64{},
	}
	st.Proposals[id] = p
	if err := g.store.Save(); err != nil {
		return nil, err
	}
	return cloneProposal(p), nil
}

// GetProposal 返回提案的深拷贝。
func (g *Governor) GetProposal(id string) (*Proposal, error) {
	g.mu.RLock()
	defer g.mu.RUnlock()
	p, ok := g.store.State().Proposals[id]
	if !ok {
		return nil, newError(CodeNotFound, "proposal not found: "+id)
	}
	return cloneProposal(p), nil
}

// ListProposals 返回全部提案的深拷贝，按 ID 排序。
func (g *Governor) ListProposals() []*Proposal {
	g.mu.RLock()
	defer g.mu.RUnlock()
	ids := make([]string, 0, len(g.store.State().Proposals))
	for id := range g.store.State().Proposals {
		ids = append(ids, id)
	}
	// IDs 形如 P000001，字典序即创建序。
	for i := 1; i < len(ids); i++ {
		for j := i; j > 0 && ids[j-1] > ids[j]; j-- {
			ids[j-1], ids[j] = ids[j], ids[j-1]
		}
	}
	out := make([]*Proposal, 0, len(ids))
	for _, id := range ids {
		out = append(out, cloneProposal(g.store.State().Proposals[id]))
	}
	return out
}

func nextProposalID(seq int64, now time.Time) string {
	return fmt.Sprintf("P%06d", seq)
}

func cloneProposal(p *Proposal) *Proposal {
	cp := *p
	cp.Snapshot = cloneSnapshot(p.Snapshot)
	cp.Actions = append([]Action(nil), p.Actions...)
	cp.Votes = make(map[Address]VoteChoice, len(p.Votes))
	for k, v := range p.Votes {
		cp.Votes[k] = v
	}
	cp.VoteHeights = make(map[Address]int64, len(p.VoteHeights))
	for k, v := range p.VoteHeights {
		cp.VoteHeights[k] = v
	}
	cp.AppliedActions = append([]AppliedAction(nil), p.AppliedActions...)
	return &cp
}
