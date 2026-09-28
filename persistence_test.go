package governance

import "testing"

// 模拟"重启"：在同一个文件上新建 Governor，验证全部结论从磁盘恢复且保持一致。
func TestRestartConsistency(t *testing.T) {
	g, path := newTestGovernor(t)
	mustAdvance(t, g, 1)
	if err := g.Mint("alice", 70); err != nil {
		t.Fatal(err)
	}
	if err := g.Mint("bob", 30); err != nil {
		t.Fatal(err)
	}
	// bob 委托给 alice，验证委托关系也能跨重启恢复。
	if err := g.Delegate("bob", "alice"); err != nil {
		t.Fatal(err)
	}
	p, err := g.CreateProposal(CreateProposalInput{
		Title: "restart", Proposer: "alice",
		SnapshotBlock: 1, VoteStartBlock: 2, VoteEndBlock: 3,
		Quorum: 0.1, Threshold: 0.5,
		Actions: []Action{{Type: ActionGrantRole, Role: "admin", Member: "carol"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	mustAdvance(t, g, 2)
	if _, err := g.Vote(p.ID, "alice", OptionYes); err != nil { // 聚合 100
		t.Fatal(err)
	}
	mustAdvance(t, g, 4)
	r1, err := g.Tally(p.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := g.Execute(p.ID); err != nil {
		t.Fatal(err)
	}
	t.Logf("before restart: outcome=%s yes=%d total=%d", r1.Outcome, r1.Yes, r1.TotalWeight)

	// === 模拟进程重启：同一文件重新加载 ===
	g2, err := NewGovernor(NewFileStore(path))
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if g2.Height() != 4 {
		t.Fatalf("height lost across restart: %d", g2.Height())
	}
	p2, err := g2.GetProposal(p.ID)
	if err != nil {
		t.Fatal(err)
	}
	if p2.Status != StatusExecuted {
		t.Fatalf("proposal status after restart = %s", p2.Status)
	}
	if p2.Snapshot.TotalWeight != 100 || p2.Snapshot.PowerOf("alice") != 100 {
		t.Fatalf("snapshot after restart: total=%d alice=%d",
			p2.Snapshot.TotalWeight, p2.Snapshot.PowerOf("alice"))
	}
	r2, err := g2.GetTally(p.ID)
	if err != nil {
		t.Fatal(err)
	}
	if r2.Yes != 100 || r2.Outcome != StatusSucceeded {
		t.Fatalf("tally changed across restart: yes=%d outcome=%s", r2.Yes, r2.Outcome)
	}
	votes, _ := g2.GetVotes(p.ID)
	if len(votes) != 1 || votes[0].Weight != 100 {
		t.Fatalf("votes lost/changed across restart: %+v", votes)
	}
	has, _ := g2.HasRole("admin", "carol")
	if !has {
		t.Fatal("executed role grant lost across restart")
	}
	t.Logf("after restart: status=%s tally yes=%d/%d votes=%d admin:carol=%v — all consistent",
		p2.Status, r2.Yes, r2.TotalWeight, len(votes), has)

	// 重启后重复执行仍必须被拒绝。
	if _, err := g2.Execute(p.ID); err == nil {
		t.Fatal("repeated execution after restart must be rejected")
	} else {
		t.Logf("post-restart repeated execution rejected: %v", err)
	}
}

// 重启后再次启动投票窗口之外的投票/计票，行为与重启前一致。
func TestRestartVotingRulesRemainEnforced(t *testing.T) {
	g, path := newTestGovernor(t)
	mustAdvance(t, g, 1)
	if err := g.Mint("alice", 50); err != nil {
		t.Fatal(err)
	}
	p, err := g.CreateProposal(CreateProposalInput{
		Title: "window", Proposer: "alice",
		SnapshotBlock: 1, VoteStartBlock: 2, VoteEndBlock: 3,
		Quorum: 0, Threshold: 0,
		Actions: []Action{{Type: ActionMint, To: "x", Amount: 1}},
	})
	if err != nil {
		t.Fatal(err)
	}

	g2, err := NewGovernor(NewFileStore(path))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := g2.Vote(p.ID, "alice", OptionYes); err == nil {
		t.Fatal("vote before start must remain rejected after restart")
	} else {
		t.Logf("post-restart early vote rejected: %v", err)
	}
}
