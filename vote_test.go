package governance

import (
	"errors"
	"testing"
)

func setupVoteProposal(t *testing.T) (*Governor, string) {
	t.Helper()
	g, _ := newTestGovernor(t)
	mustAdvance(t, g, 1)
	if err := g.Mint("alice", 70); err != nil {
		t.Fatal(err)
	}
	if err := g.Mint("bob", 30); err != nil {
		t.Fatal(err)
	}
	p, err := g.CreateProposal(CreateProposalInput{
		Title: "vote rules", Proposer: "alice",
		SnapshotBlock: 1, VoteStartBlock: 2, VoteEndBlock: 4,
		Quorum: 0.5, Threshold: 0.5,
		Actions: []Action{{Type: ActionMint, To: "x", Amount: 1}},
	})
	if err != nil {
		t.Fatal(err)
	}
	return g, p.ID
}

// 重复投票必须被拒绝，且只有第一次被计入。
func TestDuplicateVoteRejected(t *testing.T) {
	g, id := setupVoteProposal(t)
	mustAdvance(t, g, 2)
	v1, err := g.Vote(id, "alice", OptionYes)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("first vote accepted: voter=alice weight=%d option=%s", v1.Weight, v1.Option)

	_, err = g.Vote(id, "alice", OptionNo)
	if !errors.Is(err, ErrAlreadyVoted) {
		t.Fatalf("expected ErrAlreadyVoted, got %v", err)
	}
	t.Logf("duplicate vote rejected: %v", err)

	mustAdvance(t, g, 5)
	r, _ := g.Tally(id)
	t.Logf("tally yes=%d no=%d (alice's changed mind must not be counted twice)", r.Yes, r.No)
	if r.Yes != 70 || r.No != 0 {
		t.Fatalf("duplicate vote leaked into tally: yes=%d no=%d", r.Yes, r.No)
	}
}

// 无效选项被拒绝。
func TestInvalidOptionRejected(t *testing.T) {
	g, id := setupVoteProposal(t)
	mustAdvance(t, g, 2)
	_, err := g.Vote(id, "alice", VoteOption("maybe"))
	if !errors.Is(err, ErrInvalidOption) {
		t.Fatalf("expected ErrInvalidOption, got %v", err)
	}
	t.Logf("invalid option rejected: %v", err)
}

// 投票窗口外：未开始与已结束必须是两个不同的错误原因。
func TestVoteWindowEnforced(t *testing.T) {
	g, id := setupVoteProposal(t)

	mustAdvance(t, g, 1) // 投票 2~4，当前为 1
	_, errEarly := g.Vote(id, "alice", OptionYes)
	if !errors.Is(errEarly, ErrVotingNotStarted) {
		t.Fatalf("expected ErrVotingNotStarted, got %v", errEarly)
	}
	t.Logf("early vote rejected: %v", errEarly)

	mustAdvance(t, g, 5)
	_, errLate := g.Vote(id, "alice", OptionYes)
	if !errors.Is(errLate, ErrVotingClosed) {
		t.Fatalf("expected ErrVotingClosed, got %v", errLate)
	}
	t.Logf("late vote rejected: %v", errLate)
}

// 快照内无权重地址被拒绝；无权重地址不影响法定人数。
func TestVoteRequiresSnapshotPower(t *testing.T) {
	g, id := setupVoteProposal(t)
	mustAdvance(t, g, 2)
	_, errGhost := g.Vote(id, "ghost", OptionYes)
	if !errors.Is(errGhost, ErrNoVotingPower) {
		t.Fatalf("expected ErrNoVotingPower, got %v", errGhost)
	}
	t.Logf("powerless voter rejected: %v", errGhost)
}

// 法定人数不足 => defeated，且原因可解释。
func TestQuorumFailure(t *testing.T) {
	g, id := setupVoteProposal(t)
	mustAdvance(t, g, 2)
	if _, err := g.Vote(id, "bob", OptionYes); err != nil { // 30/100 参与
		t.Fatal(err)
	}
	mustAdvance(t, g, 5)
	r, err := g.Tally(id)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("quorum case: participating=%d/%d rate=%s need=%s met=%v outcome=%s",
		r.ParticipatingWeight, r.TotalWeight, pct(r.QuorumRate), pct(0.5), r.QuorumMet, r.Outcome)
	if r.Outcome != StatusDefeated || r.QuorumMet {
		t.Fatalf("expected defeated by quorum, got %s quorumMet=%v", r.Outcome, r.QuorumMet)
	}
	if _, err := g.Execute(id); !errors.Is(err, ErrQuorumNotMet) {
		t.Fatalf("execute on quorum failure should return ErrQuorumNotMet, got %v", err)
	}
	t.Logf("execute rejected with distinguishable reason: %v", err)
}

// 法定人数达标但赞成率不足 => defeated。
func TestThresholdFailure(t *testing.T) {
	g, id := setupVoteProposal(t)
	mustAdvance(t, g, 2)
	if _, err := g.Vote(id, "alice", OptionNo); err != nil { // 70 反对
		t.Fatal(err)
	}
	if _, err := g.Vote(id, "bob", OptionYes); err != nil { // 30 赞成
		t.Fatal(err)
	}
	mustAdvance(t, g, 5)
	r, _ := g.Tally(id)
	t.Logf("threshold case: approval=%s need=50.00%% thresholdMet=%v outcome=%s",
		pct(r.ApprovalRate), r.ThresholdMet, r.Outcome)
	if r.Outcome != StatusDefeated || r.ThresholdMet {
		t.Fatalf("expected defeated by threshold, got %s", r.Outcome)
	}
	_, errExec := g.Execute(id)
	if !errors.Is(errExec, ErrThresholdNotMet) {
		t.Fatalf("execute should return ErrThresholdNotMet, got %v", errExec)
	}
	t.Logf("execute rejected: %v", errExec)
}

// 投票期未结束不能计票；同一提案重复计票返回同一结论。
func TestTallyTimingAndIdempotency(t *testing.T) {
	g, id := setupVoteProposal(t)
	mustAdvance(t, g, 3)
	if _, err := g.Tally(id); !errors.Is(err, ErrVotingOngoing) {
		t.Fatalf("expected ErrVotingOngoing, got %v", err)
	}
	mustAdvance(t, g, 5)
	r1, err := g.Tally(id)
	if err != nil {
		t.Fatal(err)
	}
	r2, err := g.Tally(id)
	if err != nil {
		t.Fatal(err)
	}
	if r1 != r2 {
		t.Fatal("repeated tally must return the same persisted result")
	}
	t.Logf("repeated tally is idempotent, outcome=%s", r2.Outcome)
}
