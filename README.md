# 本地 DAO 治理：提案、投票与计票

一个**纯本地**的 DAO 治理状态机：用区块（整数高度）+ 原子落盘的 JSON 文件模拟链上状态，
不连接任何真实链或外部服务。目标是让提案从创建、投票、计票到执行的结论，
在**重启、并发、执行部分失败**等情况下都保持一致、可解释。

## 快速开始

```go
store := governance.NewFileStore("state.json")
g, _ := governance.NewGovernor(store) // 文件不存在会自动初始化

g.AdvanceTo(1)
g.Mint("alice", 60)
g.Mint("bob", 40)

p, _ := g.CreateProposal(governance.CreateProposalInput{
    Title: "grant admin", Proposer: "alice",
    SnapshotBlock:  1,   // 投票权重以 1 号区块的快照为准
    VoteStartBlock: 2,
    VoteEndBlock:   4,
    Quorum:         0.1, // 参与率下限
    Threshold:      0.5, // 赞成率下限
    Actions: []governance.Action{
        {Type: governance.ActionGrantRole, Role: "admin", Member: "carol"},
    },
})

g.AdvanceTo(2)
g.Vote(p.ID, "alice", governance.OptionYes)
g.AdvanceTo(3)
g.Vote(p.ID, "bob", governance.OptionNo)

g.AdvanceTo(5) // 投票结束后才能计票
r, _ := g.Tally(p.ID)
fmt.Println(r.Outcome, r.Reasons) // succeeded + 逐条判定依据

g.Execute(p.ID) // 仅 succeeded 可执行
```

## 状态与持久化（为什么重启不丢结论）

- 全部状态（权重账本、委托历史、提案、**物化快照**、选票、计票结论、执行结果）
  保存在单个 `State` 中，序列化为一个 JSON 文件。
- 保存采用 **临时文件 + fsync + 原子 rename**：任意时刻磁盘上要么是完整的旧状态、
  要么是完整的新状态，不会出现半截文件。
- 权重与委托都是 **append-only 历史点**（地址 → `[]BalancePoint` / `[]DelegationPoint`），
  每个历史点带区块号，可查询任意历史区块的值。变更只追加新点，从不修改旧点。
- 每次状态变更都在互斥锁内「改内存 → 整体落盘」，并发写被串行化。

## 快照规则

1. **创建提案时物化快照**：在 `SnapshotBlock` 上，把每个有正权重地址的权重，
   按当时的委托关系聚合到最终代表，生成一份独立的 `Snapshot{Powers, TotalWeight}`，
   随提案一起持久化。
2. **快照之后的一切权重变化都与该提案无关**：新增（mint）、转移（transfer）、
   销毁（burn）、甚至快照之后才出现的新地址，都不会进入本次计票。
   见 `TestSnapshotPostChangesDoNotAffectProposal`。
3. 投票时的权重**只**读取提案自带的不可变快照，不读取实时余额。
4. `SnapshotBlock` 不能是未来区块（`ErrSnapshotInFuture`）；
   快照总权重为 0 时不允许建提案（`ErrSnapshotEmpty`）。

## 委托语义

- 地址可在某区块把自身权重委托给另一个地址，支持**传递委托**
  （carol → bob → alice，则 carol、bob 的权重都归集到 alice）。
- 权重随委托链归集到**最终代表**；主动把权重委托出去的地址有效权重为 0，
  不能再自己投票，从机制上杜绝「一票两投」。
- 委托关系按区块记录，撤销委托 = 追加一个空委托点，历史关系仍可在旧区块被查到。
- **不允许自委托**（`ErrSelfDelegation`）；新增委托若会成环，整笔操作被拒绝，
  已有委托关系不受任何影响（`ErrDelegationCycle`）。
- **撤销不改写历史**：撤销委托或后续转移权重，只影响撤销之后区块的新快照；
  已经物化到旧提案里的权重归属保持稳定，旧提案的计票结论不会被悄悄改写。
  见 `TestRevokeDoesNotRewriteOldSnapshot`。

## 投票规则（失败原因可区分）

| 场景 | 返回的哨兵错误 |
|---|---|
| 同一提案重复投票 | `ErrAlreadyVoted` |
| 快照中有效权重为 0（含已委托出去、快照后才获得权重） | `ErrNoVotingPower` |
| 选项不是 yes/no/abstain | `ErrInvalidOption` |
| 当前区块 < 投票开始区块 | `ErrVotingNotStarted` |
| 当前区块 > 投票结束区块 | `ErrVotingClosed` |
| 投票期未结束就计票 | `ErrVotingOngoing` |

合法选项：`yes` / `no` / `abstain`（弃权计入参与权重，但不计入赞成/反对）。
所有错误都用 `fmt.Errorf("...: %w", sentinel)` 包装，调用方用 `errors.Is` 精确判定。

## 计票规则

- 计票读取两类**不可变**数据：提案自带快照 + 该提案下已持久化的选票，
  因此结论是确定性的，重复计票返回同一结果（幂等）。
- 参与率 = (yes + no + abstain) / 快照总权重，须 ≥ `Quorum`；
- 赞成率 = yes / (yes + no)，须 ≥ `Threshold`（弃权不进赞成率分母）；
- 两项都达标 → `succeeded`，否则 `defeated`。
- `TallyResult.Reasons` 逐条记录关键数字与判定依据（总量、参与量、参与率、
  赞成/反对/弃权、赞成率、最终结论），单测日志会打印这些依据。

## 执行规则

- 只有 `succeeded` 的提案可执行；未计票 / 法定人数不足 / 赞成率不足分别返回
  `ErrProposalNotTallied` / `ErrQuorumNotMet` / `ErrThresholdNotMet`。
- 动作按顺序应用，支持：`mint`、`burn`、`transfer`、`grant_role`、`revoke_role`，
  创建提案时即做动作参数校验。
- **幂等**：提案一旦有执行结论（成功或已标记失败），重复执行统一返回
  `ErrAlreadyExecuted`；并发执行也保证只生效一次（见
  `TestConcurrentExecutionHappensOnce`）。
- **部分失败回滚**：任一动作失败时，已成功应用的动作通过**逆操作 journal**
  按相反顺序回滚（mint↔burn、transfer 反向转回、授权↔撤权），余额与权限恢复到执行前；
  提案被明确标记为 `execution_failed` 并持久化。
  见 `TestExecutionPartialFailureRollback`。
- 若回滚本身也失败（极端异常），返回 `ErrRollbackFailed` 并同样持久化失败标记，
  不会静默留下无法解释的状态。

## 并发

`Governor` 内部用互斥锁把「改状态 + 落盘」串行化，因此：

- 同一投票者并发重复投票：恰好 1 次成功，其余 `ErrAlreadyVoted`，不重复统计；
- 不同投票者并发投票：每张都被统计，不丢失；
- 并发计票 / 并发执行：结论只产生一次。

## 验证方法

```bash
go test ./... -race -v
```

测试覆盖（测试日志会打印关键状态与判定依据）：

| 测试 | 覆盖场景 |
|---|---|
| `TestSnapshotPostChangesDoNotAffectProposal` | 快照后增发/转移/销毁/新地址均不影响旧提案 |
| `TestSnapshotFutureBlockRejected` | 拒绝未来区块快照 |
| `TestDuplicateVoteRejected` | 重复投票被拒且只计一次 |
| `TestInvalidOptionRejected` / `TestVoteWindowEnforced` / `TestVoteRequiresSnapshotPower` | 无效选项、窗口外、无权重的可区分拒绝 |
| `TestQuorumFailure` / `TestThresholdFailure` | 法定人数/门槛失败与执行拒绝 |
| `TestDelegationAggregatesPower` | 直接与传递委托的权重归集、禁止重复投票 |
| `TestDelegationCycleRejected` / `TestRevokeWithoutDelegation` | 自委托、循环委托、空撤销 |
| `TestRevokeDoesNotRewriteOldSnapshot` | 撤销委托 + 转移权重后旧提案结论不变 |
| `TestExecuteIdempotent` / `TestExecutionPartialFailureRollback` | 重复执行、部分失败回滚与终态标记 |
| `TestRestartConsistency` / `TestRestartVotingRulesRemainEnforced` | 落盘重启后提案/快照/选票/执行结论一致 |
| `TestConcurrentVotingNoDoubleCountNoLoss` / `TestConcurrentExecutionHappensOnce` / `TestConcurrentTallyHappensOnce` | 并发下不重复、不丢失、只生效一次（`-race`） |

## 文件结构

```
errors.go       哨兵错误（可区分的失败原因）与错误包装
types.go        VoteOption / Proposal / Snapshot / Action / State 等领域类型
state.go        状态初始化与反序列化规整
storage.go      原子 JSON 文件存储（tmp + fsync + rename）
ledger.go       append-only 权重账本，支持任意历史区块查询
delegation.go   委托/撤销、环检测、有效权重聚合、快照物化
proposal.go     提案创建校验、投票窗口与选票写入
tally.go        确定性计票：法定人数 + 赞成率
execution.go    动作执行、逆操作 journal 回滚、终态幂等
governor.go     门面：加锁、推进区块、落盘、查询
*_test.go       上述全部场景的测试
```
