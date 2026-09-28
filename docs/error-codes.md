# 错误码清单

所有失败都返回 `*governance.GovError`，可用类型断言读取 `Code` 区分原因，
`Reason` 给出人类可读的细节。

| Code | 触发场景 |
| --- | --- |
| `invalid_argument` | 参数缺失/非法，如金额非正、门槛超出 [0,1] |
| `not_found` | 提案不存在、撤销不存在的委托 |
| `invalid_option` | 投票选项不是 yes/no/abstain |
| `double_vote` | 同一地址对同一提案重复投票（含重启、并发） |
| `voting_not_started` | 当前高度早于投票开始高度 |
| `voting_closed` | 当前高度已到/超过投票结束高度 |
| `voting_still_open` | 投票窗口未结束就调用 Finalize |
| `no_voting_power` | 投票人在提案快照中没有正权重（含已委托者本人） |
| `self_delegation` | 委托给自己 |
| `delegation_cycle` | 新委托会形成环，或存量委托链中检测到环 |
| `insufficient_balance` | 转账/销毁/执行扣款时余额不足 |
| `proposal_not_active` | 对已定案提案投票 |
| `already_finalized` | 对非 active 提案重复定案 |
| `quorum_not_met` | （定案原因，记录在提案说明中）参与率不足 |
| `threshold_not_met` | （定案原因）赞成率不足 |
| `not_executable` | 执行未通过/未定案的提案 |
| `already_executed` | 重复执行已执行提案 |
| `execution_failed` | 执行中某动作失败，已逆向回滚，可重试 |
| `snapshot_unavailable` | 请求的快照高度无对应区块 |
