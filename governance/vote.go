package governance

// Vote 对提案投票。拒绝原因通过错误码区分：
// 提案不存在、提案未处于投票期、窗口未开始/已结束、无快照权重、
// 重复投票、无效选项。
func (g *Governor) Vote(proposalID string, voter Address, choice VoteChoice) error {
	if voter == "" {
		return newError(CodeInvalidArgument, "voter required")
	}
	if !validChoice(choice) {
		return newError(CodeInvalidOption, "vote choice must be one of yes/no/abstain")
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	st := g.store.State()
	p, ok := st.Proposals[proposalID]
	if !ok {
		return newError(CodeNotFound, "proposal not found: "+proposalID)
	}
	if p.Status != StatusActive {
		return newError(CodeProposalNotActive, "proposal is not in active voting state")
	}
	if st.Height < p.StartHeight {
		return newError(CodeVotingNotStarted, "voting window has not started")
	}
	if st.Height >= p.EndHeight {
		return newError(CodeVotingClosed, "voting window has ended")
	}
	if _, dup := p.Votes[voter]; dup {
		return newError(CodeDoubleVote, "voter has already voted on this proposal")
	}
	if p.Snapshot.Weights[voter] <= 0 {
		return newError(CodeNoVotingPower, "voter has no weight in the proposal snapshot")
	}
	p.Votes[voter] = choice
	p.VoteHeights[voter] = st.Height
	return g.store.Save()
}

// LiveTally 依据已冻结的快照实时重算当前票数（全量重算，无增量误差）。
func (g *Governor) LiveTally(proposalID string) (TallyResult, error) {
	g.mu.RLock()
	defer g.mu.RUnlock()
	p, ok := g.store.State().Proposals[proposalID]
	if !ok {
		return TallyResult{}, newError(CodeNotFound, "proposal not found: "+proposalID)
	}
	return computeTally(p), nil
}

// computeTally 从提案的投票记录与快照全量重算计票结论。
// 计票规则：
//   - 每张票按投票人在“提案快照”中的权重计值；
//   - turnout = 赞成 + 反对 + 弃权；quorum = turnout / 快照总权重；
//   - approval = 赞成 / (赞成 + 反对)，弃权不计入门槛分母；
//   - 分母为 0 时 approval 记 0。
func computeTally(p *Proposal) TallyResult {
	t := TallyResult{EligibleWeight: p.Snapshot.TotalWeight}
	for voter, choice := range p.Votes {
		w := p.Snapshot.Weights[voter]
		if w <= 0 {
			continue
		}
		switch choice {
		case VoteYes:
			t.YesWeight += w
		case VoteNo:
			t.NoWeight += w
		case VoteAbstain:
			t.AbstainWeight += w
		}
	}
	t.TurnoutWeight = t.YesWeight + t.NoWeight + t.AbstainWeight
	if t.EligibleWeight > 0 {
		t.TurnoutRatio = float64(t.TurnoutWeight) / float64(t.EligibleWeight)
	}
	nonAbstaining := t.YesWeight + t.NoWeight
	if nonAbstaining > 0 {
		t.ApprovalRatio = float64(t.YesWeight) / float64(nonAbstaining)
	}
	t.QuorumMet = t.TurnoutRatio >= p.Quorum
	t.ThresholdMet = t.ApprovalRatio >= p.Threshold
	return t
}
