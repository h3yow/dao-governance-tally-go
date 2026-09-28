package governance

import (
	"fmt"
	"path/filepath"
	"sync"
	"testing"
)

func testGovernor(t *testing.T, name string) (*Governor, func()) {
	t.Helper()
	dir := t.TempDir()
	store, err := NewStore(filepath.Join(dir, "state.json"))
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	g := NewGovernor(store)
	return g, func() { g.Close() }
}

func mustErrCode(t *testing.T, err error, want, scene string) {
	t.Helper()
	if err == nil {
		t.Fatalf("[%s] expected error code %s, got nil", scene, want)
	}
	ge, ok := err.(*GovError)
	if !ok {
		t.Fatalf("[%s] expected *GovError, got %T: %v", scene, err, err)
	}
	if ge.Code != want {
		t.Fatalf("[%s] expected error code %s, got %s (%v)", scene, want, ge.Code, err)
	}
	t.Logf("[%s] 拒绝原因符合预期: code=%s reason=%q", scene, ge.Code, ge.Reason)
}

func logTally(t *testing.T, scene string, p *Proposal, tal TallyResult) {
	t.Helper()
	t.Logf("[%s] 状态=%s 快照高度=%d 总权重=%d | 赞成=%d 反对=%d 弃权=%d 参与=%d 参与率=%.4f(门槛quorum=%.2f) 赞成率=%.4f(门槛threshold=%.2f) => quorumMet=%v thresholdMet=%v",
		scene, p.Status, p.Snapshot.BlockHeight, tal.EligibleWeight,
		tal.YesWeight, tal.NoWeight, tal.AbstainWeight, tal.TurnoutWeight,
		tal.TurnoutRatio, p.Quorum, tal.ApprovalRatio, p.Threshold,
		tal.QuorumMet, tal.ThresholdMet)
}

// 场景1：快照之后新增、转移、销毁权重不影响已创建提案的计票。
func TestSnapshotImmutability(t *testing.T) {
	g, cleanup := testGovernor(t, "snapshot")
	defer cleanup()

	must(t, g.Mint("alice", 100))
	must(t, g.Mint("bob", 100))

	p, err := g.CreateProposal("alice", CreateProposalConfig{Title: "P1", VoteWindowBlocks: 5})
	must(t, err)
	snapshotHeight := g.Height()
	t.Logf("提案 %s 创建于高度 %d，快照总权重=%d（alice=100,bob=100）", p.ID, snapshotHeight, p.Snapshot.TotalWeight)

	// 快照之后：铸币、转账、销毁都发生在提案创建之后。
	must(t, g.Mint("carol", 500))
	must(t, g.Transfer("bob", "carol", 100))
	must(t, g.Burn("alice", 100))
	t.Logf("快照后变更: carol+500, bob->carol 100, alice 销毁100; 当前高度=%d", g.Height())

	must(t, g.Vote(p.ID, "alice", VoteYes))
	must(t, g.Vote(p.ID, "bob", VoteNo))

	// carol 快照里没有权重，即使当前持有 600 也不能投票。
	err = g.Vote(p.ID, "carol", VoteYes)
	mustErrCode(t, err, CodeNoVotingPower, "快照后新增大额权重者投票")

	// alice 快照权重仍为 100，尽管她当前余额为 0。
	got, err := g.LiveTally(p.ID)
	must(t, err)
	if got.YesWeight != 100 || got.NoWeight != 100 || got.EligibleWeight != 200 {
		t.Fatalf("快照结论被改写: %+v", got)
	}
	t.Logf("快照稳定: 赞成=%d(alice快照100) 反对=%d(bob快照100) 总权重=%d，carol当前余额=%d但不计入",
		got.YesWeight, got.NoWeight, got.EligibleWeight, g.Balance("carol"))
}

// 场景2：重复投票、过期投票、无效选项、窗口后投票分别被拒绝。
func TestVoteRejections(t *testing.T) {
	g, cleanup := testGovernor(t, "vote")
	defer cleanup()
	must(t, g.Mint("alice", 100))
	must(t, g.Mint("bob", 50))

	p, err := g.CreateProposal("alice", CreateProposalConfig{Title: "P", VoteWindowBlocks: 2})
	must(t, err)

	must(t, g.Vote(p.ID, "alice", VoteYes))
	mustErrCode(t, g.Vote(p.ID, "alice", VoteNo), CodeDoubleVote, "alice重复投票")
	mustErrCode(t, g.Vote(p.ID, "dave", VoteYes), CodeNoVotingPower, "无权重账户投票")
	mustErrCode(t, g.Vote(p.ID, "bob", VoteChoice("maybe")), CodeInvalidOption, "无效选项maybe")

	// 推进两个区块使高度到达 EndHeight，窗口关闭。
	must(t, g.Mint("miner", 1))
	must(t, g.Mint("miner", 1))
	mustErrCode(t, g.Vote(p.ID, "bob", VoteYes), CodeVotingClosed, "窗口结束后投票")
	t.Logf("窗口 [%d,%d) 已关闭，当前高度=%d", p.StartHeight, p.EndHeight, g.Height())
}

// 场景3：委托影响的是之后的新快照；撤销委托不改写旧提案快照。
func TestDelegationAndRevokeStability(t *testing.T) {
	g, cleanup := testGovernor(t, "delegation")
	defer cleanup()
	must(t, g.Mint("alice", 100))
	must(t, g.Mint("bob", 100))

	// alice -> bob：bob 的有效权重变为 200，alice 不再能自己投票。
	must(t, g.Delegate("alice", "bob"))
	p1, err := g.CreateProposal("bob", CreateProposalConfig{Title: "with-delegation", VoteWindowBlocks: 10})
	must(t, err)
	t.Logf("委托后建提案: 快照 bob=%d alice=%d 总权重=%d",
		p1.Snapshot.Weights["bob"], p1.Snapshot.Weights["alice"], p1.Snapshot.TotalWeight)
	if p1.Snapshot.Weights["bob"] != 200 {
		t.Fatalf("委托未汇总到受托人: %+v", p1.Snapshot.Weights)
	}
	mustErrCode(t, g.Vote(p1.ID, "alice", VoteYes), CodeNoVotingPower, "已委托的alice自行投票")
	must(t, g.Vote(p1.ID, "bob", VoteYes)) // bob 代表 200

	// 撤销委托，再建新提案：alice 权重恢复自主，但 p1 的旧快照不变。
	must(t, g.Undelegate("alice"))
	p2, err := g.CreateProposal("alice", CreateProposalConfig{Title: "after-revoke", VoteWindowBlocks: 10})
	must(t, err)
	if p2.Snapshot.Weights["alice"] != 100 || p2.Snapshot.Weights["bob"] != 100 {
		t.Fatalf("撤销后新快照不正确: %+v", p2.Snapshot.Weights)
	}
	old, _ := g.GetProposal(p1.ID)
	if old.Snapshot.Weights["bob"] != 200 {
		t.Fatalf("撤销委托改写了历史快照: %+v", old.Snapshot.Weights)
	}
	t.Logf("撤销后: 旧提案%s快照 bob 仍=%d；新提案%s快照 alice=%d bob=%d（历史结论稳定）",
		p1.ID, old.Snapshot.Weights["bob"], p2.ID, p2.Snapshot.Weights["alice"], p2.Snapshot.Weights["bob"])
}

// 场景4：自委托与成环委托必须被拒绝（直接成环与间接成环）。
func TestDelegationCycleRejected(t *testing.T) {
	g, cleanup := testGovernor(t, "cycle")
	defer cleanup()
	mustErrCode(t, g.Delegate("alice", "alice"), CodeSelfDelegation, "自委托")

	must(t, g.Delegate("alice", "bob"))
	must(t, g.Delegate("bob", "carol"))
	mustErrCode(t, g.Delegate("carol", "alice"), CodeDelegationCycle, "carol->alice形成alice->bob->carol->alice")
	mustErrCode(t, g.Delegate("bob", "alice"), CodeDelegationCycle, "bob->alice形成alice->bob->alice")

	// 合法的改委托不应受影响（bob 改投 dave，无环）。
	must(t, g.Delegate("bob", "dave"))
	if to, ok := g.DelegateOf("bob"); !ok || to != "dave" {
		t.Fatalf("合法改委托失败: %v %v", to, ok)
	}
	t.Logf("环检测通过；bob 改委托 dave 成功，链路 alice->bob->dave 无环")
}

// 场景5：法定人数不足 / 赞成门槛不足导致 defeated；达标后 succeeded 并可执行。
func TestQuorumThresholdAndExecute(t *testing.T) {
	g, cleanup := testGovernor(t, "tally")
	defer cleanup()
	must(t, g.Mint("alice", 60))
	must(t, g.Mint("bob", 40))
	// quorum 0.5，threshold 0.6

	// 提案1：只有 alice(60) 赞成，参与率 0.6 达标，但赞成率 = 60/(60+40)... bob没投
	// => 非弃权票只有 60 赞成，赞成率 1.0，通过。
	p1, err := g.CreateProposal("alice", CreateProposalConfig{
		Title: "pass", VoteWindowBlocks: 2, Quorum: 0.5, Threshold: 0.6,
		Actions: []Action{{Kind: ActionMint, To: "alice", Amount: 10}},
	})
	must(t, err)
	must(t, g.Vote(p1.ID, "alice", VoteYes))
	must(t, g.Mint("filler", 1))
	must(t, g.Mint("filler", 1)) // 关闭窗口
	fin1, tal1, err := g.Finalize(p1.ID)
	must(t, err)
	logTally(t, "提案1", fin1, tal1)
	if fin1.Status != StatusSucceeded {
		t.Fatalf("应通过，实际 %s: %s", fin1.Status, fin1.ExecutionError)
	}
	// 执行成功并验证动作生效、不可重复执行。
	exec1, err := g.Execute(p1.ID, "executor")
	must(t, err)
	if exec1.Status != StatusExecuted || g.Balance("alice") != 70 {
		t.Fatalf("执行结果异常 status=%s alice=%d", exec1.Status, g.Balance("alice"))
	}
	_, execErr := g.Execute(p1.ID, "executor")
	mustErrCode(t, execErr, CodeAlreadyExecuted, "重复执行")
	t.Logf("提案1执行完成: alice 余额 60->%d，重复执行被拒绝", g.Balance("alice"))

	// 提案2：赞成40 反对60，参与率1.0 达标，但赞成率0.4 < 0.6 => defeated。
	p2, err := g.CreateProposal("alice", CreateProposalConfig{
		Title: "threshold-fail", VoteWindowBlocks: 2, Quorum: 0.5, Threshold: 0.6,
	})
	must(t, err)
	must(t, g.Vote(p2.ID, "bob", VoteYes))
	must(t, g.Vote(p2.ID, "alice", VoteNo))
	must(t, g.Mint("filler", 1))
	must(t, g.Mint("filler", 1))
	fin2, tal2, err := g.Finalize(p2.ID)
	must(t, err)
	logTally(t, "提案2", fin2, tal2)
	if fin2.Status != StatusDefeated {
		t.Fatalf("应未达门槛被否决，实际 %s", fin2.Status)
	}
	_, err = g.Execute(p2.ID, "executor")
	mustErrCode(t, err, CodeNotExecutable, "执行被否决提案")
}

// 场景6：执行部分失败时已执行动作被逆向回滚，状态标记 execution_failed，可重试成功。
func TestExecutionPartialFailureRollback(t *testing.T) {
	g, cleanup := testGovernor(t, "rollback")
	defer cleanup()
	must(t, g.Mint("alice", 100))
	must(t, g.Mint("bob", 100))

	// 动作1 合法铸币给 carol；动作2 从无钱账户 dave 转账，必然失败。
	actions := []Action{
		{Kind: ActionMint, To: "carol", Amount: 30},
		{Kind: ActionTransfer, From: "dave", To: "carol", Amount: 10},
	}
	p, err := g.CreateProposal("alice", CreateProposalConfig{
		Title: "partial-fail", VoteWindowBlocks: 2, Actions: actions,
	})
	must(t, err)
	must(t, g.Vote(p.ID, "alice", VoteYes))
	must(t, g.Mint("filler", 1))
	must(t, g.Mint("filler", 1))
	fin, _, err := g.Finalize(p.ID)
	must(t, err)
	if fin.Status != StatusSucceeded {
		t.Fatalf("前置: 应通过")
	}
	_, err = g.Execute(p.ID, "executor")
	mustErrCode(t, err, CodeExecutionFailed, "含失败动作的执行")
	if g.Balance("carol") != 0 {
		t.Fatalf("动作1未回滚: carol=%d", g.Balance("carol"))
	}
	failed, _ := g.GetProposal(p.ID)
	if failed.Status != StatusExecutionFailed {
		t.Fatalf("应标记 execution_failed，实际 %s", failed.Status)
	}
	t.Logf("部分失败已回滚: carol=%d, 状态=%s, 动作明细=%+v",
		g.Balance("carol"), failed.Status, failed.AppliedActions)

	// 修正环境（给 dave 钱）后重试同一提案，应当成功且只执行一次铸币补偿语义。
	must(t, g.Mint("dave", 10))
	retry, err := g.Execute(p.ID, "executor")
	must(t, err)
	if retry.Status != StatusExecuted || g.Balance("carol") != 40 || g.Balance("dave") != 0 {
		t.Fatalf("重试结果异常: status=%s carol=%d dave=%d", retry.Status, g.Balance("carol"), g.Balance("dave"))
	}
	t.Logf("重试执行成功: carol=%d(30铸币+10转账) dave=%d 状态=%s",
		g.Balance("carol"), g.Balance("dave"), retry.Status)
}

// 场景7：重启后从磁盘恢复，提案结论与快照保持一致；窗口推进正常。
func TestRestartPersistence(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")

	store, err := NewStore(path)
	must(t, err)
	g := NewGovernor(store)
	must(t, g.Mint("alice", 100))
	p, err := g.CreateProposal("alice", CreateProposalConfig{Title: "restart", VoteWindowBlocks: 3})
	must(t, err)
	must(t, g.Vote(p.ID, "alice", VoteYes))
	before, _ := g.LiveTally(p.ID)
	must(t, g.Close())

	// 重新打开同一文件，模拟进程重启。
	store2, err := NewStore(path)
	must(t, err)
	g2 := NewGovernor(store2)
	defer g2.Close()
	got, err := g2.GetProposal(p.ID)
	must(t, err)
	if got.Snapshot.TotalWeight != before.EligibleWeight || got.Snapshot.BlockHeight != p.Snapshot.BlockHeight {
		t.Fatalf("重启后快照不一致: %+v", got.Snapshot)
	}
	if got.Votes["alice"] != VoteYes || g2.Height() != g.Height() {
		t.Fatalf("重启后投票/高度丢失")
	}
	// 重复投票在重启后同样被拒绝（状态真正持久化）。
	mustErrCode(t, g2.Vote(p.ID, "alice", VoteNo), CodeDoubleVote, "重启后重复投票")
	t.Logf("重启恢复一致: 高度=%d 快照高度=%d 总权重=%d alice投票=%s",
		g2.Height(), got.Snapshot.BlockHeight, got.Snapshot.TotalWeight, got.Votes["alice"])
}

// 场景8：并发投票不会重复统计或丢失（每个不同投票人恰好一票）。
func TestConcurrentVotes(t *testing.T) {
	g, cleanup := testGovernor(t, "concurrent")
	defer cleanup()

	const n = 20
	for i := 0; i < n; i++ {
		must(t, g.Mint(Address(fmt.Sprintf("v%02d", i)), 10))
	}
	p, err := g.CreateProposal("v00", CreateProposalConfig{Title: "concurrent", VoteWindowBlocks: 1000})
	must(t, err)

	var wg sync.WaitGroup
	errs := make(chan error, n*2)
	for i := 0; i < n; i++ {
		voter := Address(fmt.Sprintf("v%02d", i))
		wg.Add(2)
		go func() { defer wg.Done(); errs <- g.Vote(p.ID, voter, VoteYes) }()
		go func() { defer wg.Done(); errs <- g.Vote(p.ID, voter, VoteNo) }() // 与赞成竞争，恰一个成功
	}
	wg.Wait()
	close(errs)
	ok, rejected := 0, 0
	for e := range errs {
		if e == nil {
			ok++
		} else {
			rejected++
		}
	}
	tal, err := g.LiveTally(p.ID)
	must(t, err)
	if ok != n || tal.TurnoutWeight != int64(n*10) || tal.YesWeight+tal.NoWeight != int64(n*10) {
		t.Fatalf("并发投票统计异常: 成功=%d 拒绝=%d 参与权重=%d", ok, rejected, tal.TurnoutWeight)
	}
	t.Logf("并发投票: %d 个投票人各投两票竞争 => 成功=%d 被拒(重复)=%d 参与权重=%d 无重复无丢失",
		n, ok, rejected, tal.TurnoutWeight)
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}
