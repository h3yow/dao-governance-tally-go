package governance

import (
	"fmt"
	"time"
)

// CreateProposalInput 创建提案的输入参数。
type CreateProposalInput struct {
	Title          string
	Proposer       string
	SnapshotBlock  int64 // 投票权重以此区块的快照为准
	VoteStartBlock int64 // 投票开始区块（含）
	VoteEndBlock   int64 // 投票结束区块（含）
	Quorum         float64
	Threshold      float64
	Actions        []Action
}

func newProposalState() map[string]*Proposal {
	return map[string]*Proposal{}
}

// validateAndCreateProposal 做参数校验、物化快照并构造提案（尚不写入状态）。
func validateAndCreateProposal(st *State, in CreateProposalInput, now time.Time) (*Proposal, error) {
	if in.Title == "" {
		return nil, wrapf(ErrInvalidArgument, "proposal title is empty")
	}
	if in.Proposer == "" {
		return nil, wrapf(ErrInvalidArgument, "proposer is empty")
	}
	if in.SnapshotBlock < 0 {
		return nil, wrapf(ErrInvalidArgument, "snapshot block %d is negative", in.SnapshotBlock)
	}
	if in.SnapshotBlock > st.CurrentBlock {
		return nil, wrapf(ErrSnapshotInFuture, "snapshot block %d > current block %d",
			in.SnapshotBlock, st.CurrentBlock)
	}
	if in.VoteStartBlock < st.CurrentBlock {
		return nil, wrapf(ErrInvalidArgument, "vote start block %d is before current block %d",
			in.VoteStartBlock, st.CurrentBlock)
	}
	if in.VoteEndBlock <= in.VoteStartBlock {
		return nil, wrapf(ErrInvalidArgument, "vote end block %d must be after start block %d",
			in.VoteEndBlock, in.VoteStartBlock)
	}
	if in.Quorum < 0 || in.Quorum > 1 {
		return nil, wrapf(ErrInvalidArgument, "quorum %.3f must be within [0,1]", in.Quorum)
	}
	if in.Threshold < 0 || in.Threshold > 1 {
		return nil, wrapf(ErrInvalidArgument, "threshold %.3f must be within [0,1]", in.Threshold)
	}
	if len(in.Actions) == 0 {
		return nil, wrapf(ErrInvalidArgument, "proposal has no actions to execute")
	}
	if err := validateActions(in.Actions); err != nil {
		return nil, err
	}

	// 关键：在创建提案时物化快照，并把快照随提案持久化。
	snap, err := materializeSnapshot(&st.Ledger, &st.Delegations, in.SnapshotBlock)
	if err != nil {
		return nil, err
	}
	if snap.TotalWeight <= 0 {
		return nil, wrapf(ErrSnapshotEmpty, "no voting weight exists at block %d", in.SnapshotBlock)
	}
	snap.CreatedAt = now

	st.Seq++
	p := &Proposal{
		ID:            fmt.Sprintf("proposal-%d", st.Seq),
		Title:         in.Title,
		Proposer:      in.Proposer,
		SnapshotBlock: in.SnapshotBlock,
		Snapshot:      snap,
		StartBlock:    in.VoteStartBlock,
		EndBlock:      in.VoteEndBlock,
		Quorum:        in.Quorum,
		Threshold:     in.Threshold,
		Actions:       in.Actions,
		Status:        StatusActive,
		CreatedAt:     now,
	}
	return p, nil
}

// castVote 对处于投票期内的提案投票。
func castVote(st *State, proposalID, voter string, option VoteOption, block int64, now time.Time) (*Vote, error) {
	if voter == "" {
		return nil, wrapf(ErrInvalidArgument, "voter is empty")
	}
	p, ok := st.Proposals[proposalID]
	if !ok {
		return nil, wrapf(ErrNotFound, "proposal %s", proposalID)
	}

	// 选项合法性先判，错误原因彼此可区分。
	if !option.valid() {
		return nil, wrapf(ErrInvalidOption, "option %q is not one of yes/no/abstain", option)
	}
	if block < p.StartBlock {
		return nil, wrapf(ErrVotingNotStarted,
			"proposal %s voting starts at block %d, current block %d", p.ID, p.StartBlock, block)
	}
	if block > p.EndBlock {
		return nil, wrapf(ErrVotingClosed,
			"proposal %s voting ended at block %d, current block %d", p.ID, p.EndBlock, block)
	}
	if ballots, exists := st.Votes[p.ID]; exists {
		if _, dup := ballots[voter]; dup {
			return nil, wrapf(ErrAlreadyVoted,
				"voter %s already voted on proposal %s", voter, p.ID)
		}
	}

	// 权重以提案自带的、创建时物化的不可快照样照为准。
	weight := p.Snapshot.PowerOf(voter)
	if weight <= 0 {
		return nil, wrapf(ErrNoVotingPower,
			"voter %s has no effective power in snapshot of block %d", voter, p.SnapshotBlock)
	}

	v := &Vote{
		ProposalID: p.ID,
		Voter:      voter,
		Option:     option,
		Weight:     weight,
		Block:      block,
		CastAt:     now,
	}
	if st.Votes[p.ID] == nil {
		st.Votes[p.ID] = map[string]*Vote{}
	}
	st.Votes[p.ID][voter] = v
	return v, nil
}
