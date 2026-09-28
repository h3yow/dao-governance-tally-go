package governance

import (
	"errors"
	"testing"
)

// 委托后权重归集到被委托人；委托人自身失去直接投票权，避免一票两投。
func TestDelegationAggregatesPower(t *testing.T) {
	g, _ := newTestGovernor(t)
	mustAdvance(t, g, 1)
	if err := g.Mint("alice", 60); err != nil {
		t.Fatal(err)
	}
	if err := g.Mint("bob", 40); err != nil {
		t.Fatal(err)
	}
	// bob 把 40 委托给 alice；carol 委托给 bob（传递委托），carol 无权重。
	if err := g.Delegate("bob", "alice"); err != nil {
		t.Fatal(err)
	}
	if err := g.Mint("carol", 25); err != nil {
		t.Fatal(err)
	}
	if err := g.Delegate("carol", "bob"); err != nil {
		t.Fatal(err)
	}

	snap, err := g.SnapshotAt(1)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("snapshot powers: alice=%d (own60+bob40), carol delegate->bob->alice adds %d; bob=%d",
		snap.PowerOf("alice"), snap.PowerOf("alice")-100, snap.PowerOf("bob"))
	if snap.PowerOf("alice") != 125 {
		t.Fatalf("alice effective power should be 125, got %d", snap.PowerOf("alice"))
	}
	if w := snap.PowerOf("bob"); w != 0 {
		t.Fatalf("bob delegated away, its own power should be 0, got %d", w)
	}

	p, err := g.CreateProposal(CreateProposalInput{
		Title: "delegated", Proposer: "alice",
		SnapshotBlock: 1, VoteStartBlock: 2, VoteEndBlock: 3,
		Quorum: 0, Threshold: 0.5,
		Actions: []Action{{Type: ActionMint, To: "x", Amount: 1}},
	})
	if err != nil {
		t.Fatal(err)
	}
	mustAdvance(t, g, 2)
	// bob 已委托，不能再自己投票。
	_, errBobVote := g.Vote(p.ID, "bob", OptionYes)
	if !errors.Is(errBobVote, ErrNoVotingPower) {
		t.Fatalf("delegator bob should not vote directly, got %v", errBobVote)
	}
	t.Logf("bob direct vote rejected after delegation: %v", errBobVote)
	// alice 一票携带 125 的聚合权重。
	v, err := g.Vote(p.ID, "alice", OptionYes)
	if err != nil {
		t.Fatal(err)
	}
	if v.Weight != 125 {
		t.Fatalf("alice vote weight should be aggregated 125, got %d", v.Weight)
	}
	t.Logf("alice vote carries aggregated weight %d", v.Weight)
}

// 自委托与形成环的委托必须被拒绝，且错误原因可区分。
func TestDelegationCycleRejected(t *testing.T) {
	g, _ := newTestGovernor(t)
	mustAdvance(t, g, 1)
	if err := g.Mint("alice", 10); err != nil {
		t.Fatal(err)
	}
	if err := g.Mint("bob", 10); err != nil {
		t.Fatal(err)
	}
	if err := g.Mint("carol", 10); err != nil {
		t.Fatal(err)
	}

	errSelf := g.Delegate("alice", "alice")
	if !errors.Is(errSelf, ErrSelfDelegation) {
		t.Fatalf("self delegation: expected ErrSelfDelegation, got %v", errSelf)
	}
	t.Logf("self delegation rejected: %v", errSelf)

	if err := g.Delegate("alice", "bob"); err != nil {
		t.Fatal(err)
	}
	if err := g.Delegate("bob", "carol"); err != nil {
		t.Fatal(err)
	}
	// carol -> alice 将形成 alice->bob->carol->alice 的环。
	errCycle := g.Delegate("carol", "alice")
	if !errors.Is(errCycle, ErrDelegationCycle) {
		t.Fatalf("expected ErrDelegationCycle, got %v", errCycle)
	}
	t.Logf("cyclic delegation carol->alice rejected: %v", errCycle)

	// 环被拒绝后，已有委托链保持不变：alice->bob->carol，
	// 因此 alice 与 bob 的权重都归集到 carol（30），alice/bob 为 0。
	snap, _ := g.SnapshotAt(1)
	t.Logf("after rejected cycle: carol=%d (alice10+bob10+own10), alice=%d bob=%d",
		snap.PowerOf("carol"), snap.PowerOf("alice"), snap.PowerOf("bob"))
	if snap.PowerOf("carol") != 30 || snap.PowerOf("alice") != 0 || snap.PowerOf("bob") != 0 {
		t.Fatalf("delegation state corrupted after rejected cycle: %+v", snap.Powers)
	}
}

// 无委托可撤时返回专门错误。
func TestRevokeWithoutDelegation(t *testing.T) {
	g, _ := newTestGovernor(t)
	mustAdvance(t, g, 1)
	errRevoke := g.Revoke("alice")
	if !errors.Is(errRevoke, ErrDelegationNotFound) {
		t.Fatalf("expected ErrDelegationNotFound, got %v", errRevoke)
	}
	t.Logf("revoke without delegation rejected: %v", errRevoke)
}

// 关键稳定性：撤销委托不会改写已经物化的旧提案快照。
func TestRevokeDoesNotRewriteOldSnapshot(t *testing.T) {
	g, _ := newTestGovernor(t)
	mustAdvance(t, g, 1)
	if err := g.Mint("alice", 60); err != nil {
		t.Fatal(err)
	}
	if err := g.Mint("bob", 40); err != nil {
		t.Fatal(err)
	}
	if err := g.Delegate("bob", "alice"); err != nil { // bob -> alice 在区块 1
		t.Fatal(err)
	}

	p, err := g.CreateProposal(CreateProposalInput{
		Title: "old", Proposer: "alice",
		SnapshotBlock: 1, VoteStartBlock: 2, VoteEndBlock: 3,
		Quorum: 0, Threshold: 0.5,
		Actions: []Action{{Type: ActionMint, To: "x", Amount: 1}},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("old proposal snapshot: alice=%d bob=%d total=%d",
		p.Snapshot.PowerOf("alice"), p.Snapshot.PowerOf("bob"), p.Snapshot.TotalWeight)

	mustAdvance(t, g, 2)
	// 撤销 bob 的委托，并把 bob 的权重转走 —— 旧快照都不该受影响。
	if err := g.Revoke("bob"); err != nil {
		t.Fatal(err)
	}
	if err := g.Transfer("bob", "carol", 40); err != nil {
		t.Fatal(err)
	}
	t.Logf("bob revoked delegation and moved 40 weight away at block 2")

	// 旧提案里 alice 仍持有 100 的聚合权重并可一票通过。
	v, err := g.Vote(p.ID, "alice", OptionYes)
	if err != nil {
		t.Fatalf("alice must retain 100 power in old snapshot: %v", err)
	}
	if v.Weight != 100 {
		t.Fatalf("old snapshot rewritten by revoke/transfer: weight=%d", v.Weight)
	}
	if _, err := g.Vote(p.ID, "bob", OptionYes); !errors.Is(err, ErrNoVotingPower) {
		t.Fatalf("bob had no direct power in the block-1 snapshot, got %v", err)
	}
	mustAdvance(t, g, 4)
	r, _ := g.Tally(p.ID)
	t.Logf("old proposal tally unchanged: yes=%d total=%d outcome=%s",
		r.Yes, r.TotalWeight, r.Outcome)
	if r.Yes != 100 || r.Outcome != StatusSucceeded {
		t.Fatalf("revocation rewrote historical tally: yes=%d outcome=%s", r.Yes, r.Outcome)
	}

	// 而新区块的快照反映的是撤销后的新关系：alice=60, bob=0(转走), carol=40。
	fresh, _ := g.SnapshotAt(4)
	t.Logf("fresh block-4 snapshot after revoke: alice=%d carol=%d bob=%d",
		fresh.PowerOf("alice"), fresh.PowerOf("carol"), fresh.PowerOf("bob"))
	if fresh.PowerOf("alice") != 60 || fresh.PowerOf("carol") != 40 || fresh.PowerOf("bob") != 0 {
		t.Fatalf("fresh snapshot does not reflect revoked delegation: %+v", fresh.Powers)
	}
}
