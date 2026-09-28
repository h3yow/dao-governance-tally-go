package governance

import (
	"errors"
	"testing"
)

// 快照规则核心：提案快照之后新增、转移、销毁的权重都不得进入本次计票。
func TestSnapshotPostChangesDoNotAffectProposal(t *testing.T) {
	g, _ := newTestGovernor(t)

	mustAdvance(t, g, 10)
	if err := g.Mint("alice", 50); err != nil {
		t.Fatal(err)
	}
	if err := g.Mint("bob", 50); err != nil {
		t.Fatal(err)
	}

	p, err := g.CreateProposal(CreateProposalInput{
		Title: "p1", Proposer: "alice",
		SnapshotBlock: 10, VoteStartBlock: 11, VoteEndBlock: 20,
		Quorum: 0.0, Threshold: 0.5,
		Actions: []Action{{Type: ActionMint, To: "treasury", Amount: 1}},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("snapshot total weight at block 10 = %d (alice=%d bob=%d)",
		p.Snapshot.TotalWeight, p.Snapshot.PowerOf("alice"), p.Snapshot.PowerOf("bob"))

	// 快照之后：alice 翻倍增发、bob 把全部权重转走、再增发一个新大户。
	mustAdvance(t, g, 11)
	if err := g.Mint("alice", 50); err != nil {
		t.Fatal(err)
	}
	if err := g.Transfer("bob", "chad", 50); err != nil {
		t.Fatal(err)
	}
	if err := g.Mint("chad", 900); err != nil {
		t.Fatal(err)
	}

	supply, _ := g.TotalSupply()
	t.Logf("after post-snapshot changes, current total supply = %d (but proposal snapshot stays 100)", supply)
	if supply != 1050 {
		t.Fatalf("current supply should be 1050, got %d", supply)
	}
	if p.Snapshot.TotalWeight != 100 {
		t.Fatalf("immutable snapshot changed: %d", p.Snapshot.TotalWeight)
	}

	// chad 是快照之后才有的大户，快照里无权，必须被拒绝。
	_, voteErr := g.Vote(p.ID, "chad", OptionYes)
	if !errors.Is(voteErr, ErrNoVotingPower) {
		t.Fatalf("chad should have no snapshot power, got err=%v", voteErr)
	}
	t.Logf("chad vote rejected as expected: %v", voteErr)

	if _, err := g.Vote(p.ID, "bob", OptionYes); err != nil {
		t.Fatalf("bob's snapshot power must remain 50: %v", err)
	}
	if _, err := g.Vote(p.ID, "alice", OptionYes); err != nil {
		t.Fatalf("alice's snapshot power must remain 50, not 100: %v", err)
	}

	mustAdvance(t, g, 21)
	r, err := g.Tally(p.ID)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("tally yes=%d participating=%d/%d quorum=%s approval=%s outcome=%s",
		r.Yes, r.ParticipatingWeight, r.TotalWeight, pct(r.QuorumRate), pct(r.ApprovalRate), r.Outcome)
	if r.TotalWeight != 100 || r.Yes != 100 {
		t.Fatalf("post-snapshot weight leaked into tally: total=%d yes=%d", r.TotalWeight, r.Yes)
	}
}

// 拒绝引用未来区块作为快照。
func TestSnapshotFutureBlockRejected(t *testing.T) {
	g, _ := newTestGovernor(t)
	mustAdvance(t, g, 1)
	if err := g.Mint("alice", 10); err != nil {
		t.Fatal(err)
	}
	_, err := g.CreateProposal(CreateProposalInput{
		Title: "future", Proposer: "alice",
		SnapshotBlock: 5, VoteStartBlock: 6, VoteEndBlock: 8,
		Quorum: 0, Threshold: 0,
		Actions: []Action{{Type: ActionMint, To: "x", Amount: 1}},
	})
	if !errors.Is(err, ErrSnapshotInFuture) {
		t.Fatalf("expected ErrSnapshotInFuture, got %v", err)
	}
	t.Logf("future snapshot rejected: %v", err)
}
