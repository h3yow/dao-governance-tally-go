package governance

import (
	"errors"
	"testing"
)

// newTestGovernor 在临时目录上构造一个落盘 Governor。
func newTestGovernor(t *testing.T) (*Governor, string) {
	t.Helper()
	dir := t.TempDir()
	store := NewFileStore(dir + "/state.json")
	g, err := NewGovernor(store)
	if err != nil {
		t.Fatalf("NewGovernor: %v", err)
	}
	return g, store.Path()
}

func mustAdvance(t *testing.T, g *Governor, block int64) {
	t.Helper()
	if err := g.AdvanceTo(block); err != nil {
		t.Fatalf("AdvanceTo(%d): %v", block, err)
	}
}

// 最小 happy path：发权重 -> 建提案 -> 两人投票 -> 计票 -> 执行。
func TestHappyPathProposalLifecycle(t *testing.T) {
	g, _ := newTestGovernor(t)

	mustAdvance(t, g, 1)
	if err := g.Mint("alice", 60); err != nil {
		t.Fatalf("mint alice: %v", err)
	}
	if err := g.Mint("bob", 40); err != nil {
		t.Fatalf("mint bob: %v", err)
	}

	p, err := g.CreateProposal(CreateProposalInput{
		Title:          "grant admin role",
		Proposer:       "alice",
		SnapshotBlock:  1,
		VoteStartBlock: 2,
		VoteEndBlock:   4,
		Quorum:         0.1,
		Threshold:      0.5,
		Actions:        []Action{{Type: ActionGrantRole, Role: "admin", Member: "carol"}},
	})
	if err != nil {
		t.Fatalf("create proposal: %v", err)
	}
	t.Logf("proposal created: id=%s snapshotBlock=%d totalWeight=%d",
		p.ID, p.SnapshotBlock, p.Snapshot.TotalWeight)

	mustAdvance(t, g, 2)
	if _, err := g.Vote(p.ID, "alice", OptionYes); err != nil {
		t.Fatalf("alice vote: %v", err)
	}
	mustAdvance(t, g, 3)
	if _, err := g.Vote(p.ID, "bob", OptionNo); err != nil {
		t.Fatalf("bob vote: %v", err)
	}

	mustAdvance(t, g, 5)
	r, err := g.Tally(p.ID)
	if err != nil {
		t.Fatalf("tally: %v", err)
	}
	for _, reason := range r.Reasons {
		t.Logf("tally: %s", reason)
	}
	if r.Outcome != StatusSucceeded {
		t.Fatalf("expected succeeded, got %s", r.Outcome)
	}

	if _, err := g.Execute(p.ID); err != nil {
		t.Fatalf("execute: %v", err)
	}
	has, _ := g.HasRole("admin", "carol")
	if !has {
		t.Fatal("carol should have admin role after execution")
	}
	t.Logf("executed: carol is admin=%v", has)

	// errors.Is 冒烟：确认哨兵错误仍可判定。
	if !errors.Is(wrapf(ErrAlreadyVoted, "x"), ErrAlreadyVoted) {
		t.Fatal("errors.Is on wrapped sentinel failed")
	}
}
