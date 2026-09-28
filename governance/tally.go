package governance

// Finalize 在投票窗口结束后对提案计票，决定 succeeded / defeated。
// 计票是全量重算且幂等：已经定案的提案返回 CodeAlreadyFinalized。
func (g *Governor) Finalize(proposalID string) (*Proposal, TallyResult, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	st := g.store.State()
	p, ok := st.Proposals[proposalID]
	if !ok {
		return nil, TallyResult{}, newError(CodeNotFound, "proposal not found: "+proposalID)
	}
	if p.Status != StatusActive {
		return cloneProposal(p), p.Tally, newError(CodeAlreadyFinalized, "proposal already finalized as "+string(p.Status))
	}
	if st.Height < p.EndHeight {
		return nil, TallyResult{}, newError(CodeVotingStillOpen, "voting window is still open")
	}
	t := computeTally(p)
	p.Tally = t
	switch {
	case !t.QuorumMet:
		p.Status = StatusDefeated
		p.ExecutionError = "defeated: quorum not met"
	case !t.ThresholdMet:
		p.Status = StatusDefeated
		p.ExecutionError = "defeated: approval threshold not met"
	default:
		p.Status = StatusSucceeded
		p.ExecutionError = ""
	}
	if err := g.store.Save(); err != nil {
		return nil, TallyResult{}, err
	}
	return cloneProposal(p), t, nil
}
