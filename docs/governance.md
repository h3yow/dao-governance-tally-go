# 治理规则说明

## 1. 区块与状态模型

- 每次铸币 (`Mint`)、转账 (`Transfer`)、销毁 (`Burn`)、委托 (`Delegate`)、
  撤销 (`Undelegate`) 都是一笔写操作，产生一个**新区块**，链高度 +1。
- 每个区块保存该操作执行后的**余额表与委托表的深拷贝**，历史区块只追加、
  永不修改。
- 每次状态变更后，整份状态通过 “写临时文件 + `rename`” 原子落盘到 JSON。
  进程崩溃时只会读到完整的旧文件或新文件，不会读到半截内容。重启后从文件
  恢复，结论与重启前一致（见 `TestRestartPersistence`）。

## 2. 快照规则（投票权重以创建时高度为准）

- 创建提案时，在**当前链高度**立即计算并冻结一份 `Snapshot`：
  - 沿委托链把每个人的余额汇总到其**最终受托人**；
  - 只有最终受托人的地址在快照里拥有权重（`A -> B -> C` 时，A、B 的
    余额都计入 C，A/B 本人不能投票）；
  - 记录 `BlockHeight`、各地址权重与总权重。
- 快照是值拷贝，之后的**铸币、转账、销毁、委托、撤销都不会影响它**。
  因此：
  - 快照之后买到/收到大量权重的账户对旧提案没有投票权；
  - 快照后销毁权重的人，其旧票仍按快照权重计入；
  - 旧提案的计票结论不会被后续权重变化悄悄改写。
- 快照之后的变更仍正常产生区块，**之后新建**的提案会使用新高度的快照。
- 验证：`TestSnapshotImmutability`、`TestDelegationAndRevokeStability`。

## 3. 委托语义

- `Delegate(from, to)` 建立 `from -> to` 的直接委托边；委托**只影响之后
  新建提案的快照**。
- 约束：
  - 不允许自委托（错误码 `self_delegation`）；
  - 不允许形成环：沿 `to` 的现有委托链向上走，若回到链上已有节点则拒绝
    （错误码 `delegation_cycle`）。
- `Undelegate(from)` 删除委托边。撤销后新建的提案按新链路计权；**已经
  生成的历史快照值保持不变**，这是通过“区块深拷贝 + 快照值拷贝”双重保证
  的，而不是引用当前委托表。
- 计算有效权重时若在存量数据里检测到环，返回 `delegation_cycle` 而不是
  静默产出错误权重。
- 验证：`TestDelegationCycleRejected`（自委托、直接成环、间接成环、
  合法改委托）。

## 4. 投票规则

- 可投选项仅 `yes` / `no` / `abstain`，其他值返回 `invalid_option`。
- 投票窗口为区块高度区间 `[StartHeight, EndHeight)`：
  - 未开始 -> `voting_not_started`；当前高度 `>= EndHeight` -> `voting_closed`。
- 同一提案同一地址只能投一次，重复投票返回 `double_vote`（重启后、
  并发时同样成立——投票在写锁内判定）。
- 投票人必须在提案快照中拥有正权重，否则返回 `no_voting_power`。
- 每张票按**快照权重**计值，与投票时的实时余额无关。
- 并发安全：所有投票、计票、执行都经过 `Governor` 的同一把 `sync.RWMutex`。
  `TestConcurrentVotes` 让每个投票人并发竞争投两次，断言恰好一票成功，
  总参与权重不重不漏。

## 5. 计票规则

- 计票为**全量重算**（遍历投票记录 × 快照权重），不做增量累计，因此
  不存在并发增量计数导致的重复统计或丢失，也方便解释与审计。
- 指标：
  - `TurnoutWeight = yes + no + abstain`；
  - `TurnoutRatio = TurnoutWeight / 快照总权重`（法定人数/quorum）；
  - `ApprovalRatio = yes / (yes + no)`（弃权计入参与率、不计入赞成率分母；
    分母为 0 时赞成率记 0）。
- `Finalize` 只能在窗口结束后调用（窗口内返回 `voting_still_open`），
  且幂等：重复定案返回 `already_finalized`。
- 定案结果：
  - `TurnoutRatio < Quorum` -> `defeated`（quorum 不足）；
  - 否则 `ApprovalRatio < Threshold` -> `defeated`（门槛不足）；
  - 两者都满足 -> `succeeded`，进入可执行状态。
- `LiveTally` 可在投票期随时查看当前计票与判定依据，不改变状态。

## 6. 执行规则

- 只有 `succeeded` 可以执行；`defeated`/`active` 返回 `not_executable`。
- 动作顺序应用，当前内置三类动作：
  - `mint`：给地址铸币；
  - `transfer`：从某地址扣款并转入另一地址，余额不足即失败；
  - `set_quorum`：修改系统默认 quorum（记录旧值用于回滚）。
- **原子性与部分失败**：
  1. 所有动作先在内存中顺序应用，期间不落盘；
  2. 任一动作失败，已应用动作按**逆序补偿**（mint 扣回、transfer 反向、
     set_quorum 还原旧值）；
  3. 提案标记为 `execution_failed`，`AppliedActions` 记录每一步是否
     applied / undone 与失败原因，然后一次性落盘——磁盘上不会出现
     “执行了一半”的状态；
  4. 修正环境后可对同一提案重试，重试成功转为 `executed`。
- **不可重复执行**：`executed` 状态再次执行返回 `already_executed`。
- 执行同样在写锁内，并发执行不会重复应用动作。
- 验证：`TestExecutionPartialFailureRollback`（第二动作失败后第一动作
  被回滚、补偿后重试成功）、`TestQuorumThresholdAndExecute`（重复执行
  被拒绝）。

## 7. 状态流转

```
                 Finalize(quorum+threshold 达标)
   active ───────────────────────────────────▶ succeeded ──Execute成功──▶ executed
     │                                            │                         ▲
     │ Finalize(quorum 或 threshold 不达标)        │ Execute 部分失败(已回滚)  │ 重试成功
     ▼                                            ▼                         │
  defeated                                  execution_failed ───────────────┘
```

## 8. 验证方法

```bash
# 全量测试，-v 会打印每个场景的关键状态与判定依据（快照高度、权重、
# 各选项票数、参与率/赞成率与门槛、拒绝错误码等）
go test -v ./governance/

# 端到端演示：委托、快照后变更、被拒绝的投票、计票与执行
go run ./cmd/demo

# 静态检查
go vet ./...
```

测试覆盖：快照后新增/转移/销毁权重、重复投票、过期投票、无效选项、
循环委托（自环/直接环/间接环）、撤销委托后旧快照稳定、quorum 与
门槛判定、部分失败回滚与重试、重复执行、重启恢复、并发投票一致性。
