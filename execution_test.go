package governance

import (
	"errors"
	"testing"
)

// 先造一个已计票成功、可执行的提案。
func setupExecutable(t *testing.T, g *Governor, endBlock int64) string {
	t.Helper()
	p, err := g.CreateProposal(CreateProposalInput{
		Title: "exec", Proposer: "alice",
		SnapshotBlock: g.Height(), VoteStartBlock: g.Height() + 1, VoteEndBlock: endBlock,
		Quorum: 0, Threshold: 0.5,
		Actions: []Action{
			{Type: ActionGrantRole, Role: "admin", Member: "dave"},
			{Type: ActionMint, To: "treasury", Amount: 100},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	mustAdvance(t, g, g.Height()+1)
	if _, err := g.Vote(p.ID, "alice", OptionYes); err != nil {
		t.Fatal(err)
	}
	mustAdvance(t, g, endBlock+1)
	r, err := g.Tally(p.ID)
	if err != nil {
		t.Fatal(err)
	}
	if r.Outcome != StatusSucceeded {
		t.Fatalf("setup expected succeeded, got %s", r.Outcome)
	}
	return p.ID
}

// 同一提案不能被重复执行；执行只生效一次。
func TestExecuteIdempotent(t *testing.T) {
	g, _ := newTestGovernor(t)
	mustAdvance(t, g, 1)
	if err := g.Mint("alice", 100); err != nil {
		t.Fatal(err)
	}
	id := setupExecutable(t, g, 3)

	res, err := g.Execute(id)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("first execution: status=%s applied=%d", res.Status, len(res.Applied))
	supplyAfter, _ := g.TotalSupply()

	_, errAgain := g.Execute(id)
	if !errors.Is(errAgain, ErrAlreadyExecuted) {
		t.Fatalf("expected ErrAlreadyExecuted, got %v", errAgain)
	}
	t.Logf("repeated execution rejected: %v", errAgain)

	supplyAfter2, _ := g.TotalSupply()
	if supplyAfter != supplyAfter2 {
		t.Fatalf("repeated execution changed supply: %d -> %d", supplyAfter, supplyAfter2)
	}
	has, _ := g.HasRole("admin", "dave")
	if !has {
		t.Fatal("dave should keep admin role")
	}
	stored, _ := g.GetExecution(id)
	if stored.Status != StatusExecuted {
		t.Fatalf("persisted execution status = %s", stored.Status)
	}
}

// 未计票就执行 -> 专门错误。
func TestExecuteBeforeTally(t *testing.T) {
	g, _ := newTestGovernor(t)
	mustAdvance(t, g, 1)
	if err := g.Mint("alice", 10); err != nil {
		t.Fatal(err)
	}
	p, err := g.CreateProposal(CreateProposalInput{
		Title: "x", Proposer: "alice",
		SnapshotBlock: 1, VoteStartBlock: 2, VoteEndBlock: 3,
		Quorum: 0, Threshold: 0,
		Actions: []Action{{Type: ActionMint, To: "x", Amount: 1}},
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = g.Execute(p.ID)
	if !errors.Is(err, ErrProposalNotTallied) {
		t.Fatalf("expected ErrProposalNotTallied, got %v", err)
	}
	t.Logf("execute before tally rejected: %v", err)
}

// 部分失败必须回滚：前序 grant_role 与 mint 都要被撤销，余额/权限不留脏状态；
// 提案被明确标记 execution_failed，之后重试执行仍然被拒绝（终态幂等）。
func TestExecutionPartialFailureRollback(t *testing.T) {
	g, _ := newTestGovernor(t)
	mustAdvance(t, g, 1)
	if err := g.Mint("alice", 100); err != nil {
		t.Fatal(err)
	}

	// 构造提案：前两个动作成功，第三个 burn 超过 alice 余额必然失败。
	p, err := g.CreateProposal(CreateProposalInput{
		Title: "rollback", Proposer: "alice",
		SnapshotBlock: 1, VoteStartBlock: 2, VoteEndBlock: 3,
		Quorum: 0, Threshold: 0.5,
		Actions: []Action{
			{Type: ActionGrantRole, Role: "admin", Member: "dave"},
			{Type: ActionMint, To: "treasury", Amount: 100},
			{Type: ActionBurn, From: "alice", Amount: 9999}, // 失败点
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	mustAdvance(t, g, 2)
	if _, err := g.Vote(p.ID, "alice", OptionYes); err != nil {
		t.Fatal(err)
	}
	mustAdvance(t, g, 4)
	if _, err := g.Tally(p.ID); err != nil {
		t.Fatal(err)
	}

	supplyBefore, _ := g.TotalSupply()
	res, err := g.Execute(p.ID)
	if !errors.Is(err, ErrExecutionFailed) {
		t.Fatalf("expected ErrExecutionFailed, got %v", err)
	}
	t.Logf("execution failed at action[%d], reason: %s", res.FailedIndex, res.Reason)

	// 回滚后：授权被撤销、mint 被反向 burn，总量恢复。
	has, _ := g.HasRole("admin", "dave")
	if has {
		t.Fatal("grant_role must be rolled back: dave must not have admin")
	}
	treasury, _ := g.Balance("treasury")
	if treasury != 0 {
		t.Fatalf("mint must be rolled back: treasury balance=%d", treasury)
	}
	supplyAfter, _ := g.TotalSupply()
	if supplyAfter != supplyBefore {
		t.Fatalf("supply inconsistent after rollback: before=%d after=%d", supplyBefore, supplyAfter)
	}
	aliceBal, _ := g.Balance("alice")
	if aliceBal != 100 {
		t.Fatalf("alice balance inconsistent after rollback: %d", aliceBal)
	}
	t.Logf("rollback verified: admin=dave=%v treasury=%d supply=%d alice=%d",
		has, treasury, supplyAfter, aliceBal)

	stored, _ := g.GetExecution(p.ID)
	if stored.Status != StatusExecutionFailed {
		t.Fatalf("expected execution_failed persisted, got %s", stored.Status)
	}
	// 终态：失败后再次执行必须被拒绝，结论不会被悄悄翻成成功。
	_, errRetry := g.Execute(p.ID)
	if !errors.Is(errRetry, ErrAlreadyExecuted) {
		t.Fatalf("re-executing failed proposal must be ErrAlreadyExecuted, got %v", errRetry)
	}
	t.Logf("re-execution after failure rejected: %v", errRetry)
}
