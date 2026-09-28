// Command demo 跑一遍本地 DAO 治理的完整流程：
// 铸币 -> 委托 -> 建提案（冻结快照）-> 快照后变更 -> 投票 ->
// 计票定案 -> 执行（含部分失败回滚与重试）。全程状态落盘。
package main

import (
	"fmt"
	"path/filepath"

	gov "github.com/h3yow/dao-governance-tally-go/governance"
)

func main() {
	path := filepath.Join(".", "demo-state.json")
	store, err := gov.NewStore(path)
	if err != nil {
		panic(err)
	}
	g := gov.NewGovernor(store)
	defer g.Close()

	must(g.Mint("alice", 100))
	must(g.Mint("bob", 100))
	must(g.Mint("carol", 100))
	fmt.Printf("初始权重: alice=100 bob=100 carol=100, 高度=%d\n", g.Height())

	// carol 把投票权委托给 bob。
	must(g.Delegate("carol", "bob"))
	fmt.Println("委托: carol -> bob（bob 有效权重变为 200）")

	p, err := g.CreateProposal("alice", gov.CreateProposalConfig{
		Title:            "给 dave 铸币 50",
		Quorum:           0.5,
		Threshold:        0.5,
		VoteWindowBlocks: 4,
		Actions:          []gov.Action{{Kind: gov.ActionMint, To: "dave", Amount: 50}},
	})
	if err != nil {
		panic(err)
	}
	fmt.Printf("提案 %s 创建于高度 %d，快照总权重=%d（bob=%d, alice=%d）\n",
		p.ID, p.Snapshot.BlockHeight, p.Snapshot.TotalWeight,
		p.Snapshot.Weights["bob"], p.Snapshot.Weights["alice"])

	// 快照之后的权重变化不影响本提案。
	must(g.Mint("latecomer", 9999))
	must(g.Transfer("alice", "latecomer", 100))
	fmt.Println("快照后: latecomer 获得 9999+100，但对本提案无权重")

	must(g.Vote(p.ID, "bob", gov.VoteYes)) // bob 代表自己+carol = 200
	must(g.Vote(p.ID, "alice", gov.VoteNo))
	if err := g.Vote(p.ID, "latecomer", gov.VoteYes); err != nil {
		fmt.Printf("latecomer 投票被拒绝（预期）: %v\n", err)
	}
	tal, _ := g.LiveTally(p.ID)
	fmt.Printf("实时计票: 赞成=%d 反对=%d 参与率=%.2f 赞成率=%.2f\n",
		tal.YesWeight, tal.NoWeight, tal.TurnoutRatio, tal.ApprovalRatio)

	// 推进区块关闭投票窗口。
	for i := 0; i < 4; i++ {
		must(g.Mint("clock", 1))
	}
	fin, tal, err := g.Finalize(p.ID)
	if err != nil {
		panic(err)
	}
	fmt.Printf("定案: 状态=%s quorumMet=%v thresholdMet=%v\n",
		fin.Status, tal.QuorumMet, tal.ThresholdMet)

	executed, err := g.Execute(p.ID, "scheduler")
	if err != nil {
		panic(err)
	}
	fmt.Printf("执行: 状态=%s, dave 当前余额=%d\n", executed.Status, g.Balance("dave"))
	fmt.Printf("状态已持久化到 %s，重启进程后结论不变\n", path)
}

func must(err error) {
	if err != nil {
		panic(err)
	}
}
