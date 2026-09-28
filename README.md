# 本地 DAO 治理：提案、投票与计票

一个纯本地、无外部依赖的 Go 实现，用本地 JSON 存储模拟链上状态，
演示提案从创建、投票、计票到执行的完整流程，并保证结论在重启与
执行异常情况下一致、可解释、可复现。

## 快速开始

```bash
go run ./cmd/demo        # 端到端演示
go test -v ./governance/ # 带关键状态日志的单测
go vet ./...
```

演示与测试的状态都写在各自的临时目录 / 数据文件中；重新打开同一
状态文件即等价于“进程重启”，提案、投票、快照与执行记录全部恢复。

## 代码结构

| 文件 | 职责 |
| --- | --- |
| `governance/types.go` | 区块、快照、提案、动作、计票等核心类型 |
| `governance/errors.go` | 带稳定错误码的 `GovError`，失败原因可区分 |
| `governance/store.go` | JSON 持久化，temp 文件 + rename 原子写 |
| `governance/governor.go` | 门面、读写锁、区块追加 |
| `governance/snapshot.go` | 快照计算、委托链展开、环检测 |
| `governance/delegation.go` | 铸币/转账/销毁、委托/撤销 |
| `governance/proposal.go` | 创建提案并冻结快照、查询 |
| `governance/vote.go` | 投票校验与全量重算计票 |
| `governance/tally.go` | 窗口结束后定案（succeeded/defeated） |
| `governance/execute.go` | 顺序执行、逆向回滚、重试 |

详细规则见 [docs/governance.md](docs/governance.md)，错误码清单见
[docs/error-codes.md](docs/error-codes.md)。
