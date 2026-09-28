package governance

import "time"

// tallyProposal 在投票结束后计票。
// 计票只读取两类不可变数据：提案自带快照、该提案下已持久化的选票，
// 因此结果是确定性的；同一提案重复计票直接返回已持久化的同一结论。
func tallyProposal(st *State, id string, now time.Time) (*TallyResult, error) {
	p, ok := st.Proposals[id]
	if !ok {
		return nil, wrapf(ErrNotFound, "proposal %s", id)
	}
	// 已终态（计票成功/失败/已执行）则不允许重新计票。
	if t, done := st.Tallies[id]; done {
		return t, nil
	}
	if st.CurrentBlock <= p.EndBlock {
		return nil, wrapf(ErrVotingOngoing,
			"proposal %s voting ends at block %d, current block %d",
			id, p.EndBlock, st.CurrentBlock)
	}

	r := &TallyResult{
		ProposalID:  id,
		TotalWeight: p.Snapshot.TotalWeight,
	}
	// 遍历选票时按地址排序，保证统计过程与日志可复现。
	ballots := st.Votes[id]
	voters := make([]string, 0, len(ballots))
	for vtr := range ballots {
		voters = append(voters, vtr)
	}
	// 计票基于持久化选票，每张票只被统计一次（map 天然去重）。
	for _, vtr := range voters {
		v := ballots[vtr]
		switch v.Option {
		case OptionYes:
			r.Yes += v.Weight
		case OptionNo:
			r.No += v.Weight
		case OptionAbstain:
			r.Abstain += v.Weight
		}
	}
	r.ParticipatingWeight = r.Yes + r.No + r.Abstain

	if r.TotalWeight > 0 {
		r.QuorumRate = float64(r.ParticipatingWeight) / float64(r.TotalWeight)
	}
	if counted := r.Yes + r.No; counted > 0 {
		r.ApprovalRate = float64(r.Yes) / float64(counted)
	}

	r.QuorumMet = r.QuorumRate >= p.Quorum
	r.ThresholdMet = r.ApprovalRate >= p.Threshold

	r.Reasons = []string{
		"snapshot block " + itoa(p.SnapshotBlock) +
			", total weight " + itoa(r.TotalWeight),
		"participating weight " + itoa(r.ParticipatingWeight) +
			", quorum rate " + pct(r.QuorumRate) +
			", required quorum " + pct(p.Quorum),
		"yes " + itoa(r.Yes) + ", no " + itoa(r.No) + ", abstain " + itoa(r.Abstain) +
			", approval rate " + pct(r.ApprovalRate) +
			", required threshold " + pct(p.Threshold),
	}

	switch {
	case !r.QuorumMet:
		r.Outcome = StatusDefeated
		r.Reasons = append(r.Reasons, "defeated: quorum not met")
	case !r.ThresholdMet:
		r.Outcome = StatusDefeated
		r.Reasons = append(r.Reasons, "defeated: approval threshold not met")
	default:
		r.Outcome = StatusSucceeded
		r.Reasons = append(r.Reasons, "succeeded: quorum and threshold both met")
	}
	r.TalliedAt = now

	p.Status = r.Outcome
	st.Tallies[id] = r
	return r, nil
}

func newVoteMap() map[string]map[string]*Vote {
	return map[string]map[string]*Vote{}
}
