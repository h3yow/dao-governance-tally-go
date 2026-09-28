package governance

import "time"

// VoteOption 是合法的投票选项。
type VoteOption string

const (
	OptionYes     VoteOption = "yes"
	OptionNo      VoteOption = "no"
	OptionAbstain VoteOption = "abstain"
)

func (o VoteOption) valid() bool {
	return o == OptionYes || o == OptionNo || o == OptionAbstain
}

// ProposalStatus 提案生命周期状态。
type ProposalStatus string

const (
	StatusActive          ProposalStatus = "active"
	StatusSucceeded       ProposalStatus = "succeeded"
	StatusDefeated        ProposalStatus = "defeated"
	StatusExecuted        ProposalStatus = "executed"
	StatusExecutionFailed ProposalStatus = "execution_failed"
)

// ActionType 提案可执行的链上动作（本地模拟）。
type ActionType string

const (
	ActionMint       ActionType = "mint"
	ActionBurn       ActionType = "burn"
	ActionTransfer   ActionType = "transfer"
	ActionGrantRole  ActionType = "grant_role"
	ActionRevokeRole ActionType = "revoke_role"
)

// BalancePoint 是某地址在某个区块的权重值（append-only 历史点）。
type BalancePoint struct {
	Block  int64 `json:"block"`
	Weight int64 `json:"weight"`
}

// LedgerState 权重账本：全量历史点，支持任意历史区块查询。
type LedgerState struct {
	Balances    map[string][]BalancePoint `json:"balances"`
	TotalSupply []BalancePoint            `json:"totalSupply"`
}

// DelegationPoint 记录一次委托变更；Delegatee 为空串表示撤销。
type DelegationPoint struct {
	Block     int64  `json:"block"`
	Delegatee string `json:"delegatee"`
}

// DelegationState append-only 的委托变更历史。
type DelegationState struct {
	Entries map[string][]DelegationPoint `json:"entries"`
}

// Snapshot 是提案创建时物化（materialized）的投票权重快照，
// 一旦生成便随提案一起持久化，后续任何权重/委托变化都不会改写它。
type Snapshot struct {
	Block       int64            `json:"block"`
	TotalWeight int64            `json:"totalWeight"`
	Powers      map[string]int64 `json:"powers"` // 有效投票地址 -> 聚合后的权重
	CreatedAt   time.Time        `json:"createdAt"`
}

// PowerOf 返回某地址在该快照中的有效权重。
func (s *Snapshot) PowerOf(addr string) int64 { return s.Powers[addr] }

// Action 是提案执行的单个原子动作。
type Action struct {
	Type   ActionType `json:"type"`
	From   string     `json:"from,omitempty"`
	To     string     `json:"to,omitempty"`
	Amount int64      `json:"amount,omitempty"`
	Role   string     `json:"role,omitempty"`
	Member string     `json:"member,omitempty"`
}

// Proposal 提案。
type Proposal struct {
	ID            string         `json:"id"`
	Title         string         `json:"title"`
	Proposer      string         `json:"proposer"`
	SnapshotBlock int64          `json:"snapshotBlock"`
	Snapshot      *Snapshot      `json:"snapshot"`
	StartBlock    int64          `json:"startBlock"`
	EndBlock      int64          `json:"endBlock"`
	Quorum        float64        `json:"quorum"`    // 参与权重 / 快照总权重 的下限
	Threshold     float64        `json:"threshold"` // yes / (yes+no) 的下限
	Actions       []Action       `json:"actions"`
	Status        ProposalStatus `json:"status"`
	CreatedAt     time.Time      `json:"createdAt"`
}

// Vote 一张已投出的票；Weight 为投票时依据快照认定的有效权重。
type Vote struct {
	ProposalID string     `json:"proposalId"`
	Voter      string     `json:"voter"`
	Option     VoteOption `json:"option"`
	Weight     int64      `json:"weight"`
	Block      int64      `json:"block"`
	CastAt     time.Time  `json:"castAt"`
}

// TallyResult 计票结论与判定依据。
type TallyResult struct {
	ProposalID          string         `json:"proposalId"`
	Yes                 int64          `json:"yes"`
	No                  int64          `json:"no"`
	Abstain             int64          `json:"abstain"`
	ParticipatingWeight int64          `json:"participatingWeight"`
	TotalWeight         int64          `json:"totalWeight"`
	QuorumRate          float64        `json:"quorumRate"`
	ApprovalRate        float64        `json:"approvalRate"`
	QuorumMet           bool           `json:"quorumMet"`
	ThresholdMet        bool           `json:"thresholdMet"`
	Outcome             ProposalStatus `json:"outcome"`
	Reasons             []string       `json:"reasons"`
	TalliedAt           time.Time      `json:"talliedAt"`
}

// ExecutionResult 执行结果（成功或回滚后的失败），随状态持久化。
type ExecutionResult struct {
	ProposalID  string         `json:"proposalId"`
	Status      ProposalStatus `json:"status"`
	Applied     []Action       `json:"applied,omitempty"`
	FailedIndex int            `json:"failedIndex"`
	Reason      string         `json:"reason,omitempty"`
	Block       int64          `json:"block"`
	ExecutedAt  time.Time      `json:"executedAt"`
}

// State 是本地模拟的"链上状态"根对象，整体 JSON 持久化。
type State struct {
	Version      int                         `json:"version"`
	CurrentBlock int64                       `json:"currentBlock"`
	Ledger       LedgerState                 `json:"ledger"`
	Delegations  DelegationState             `json:"delegations"`
	Roles        map[string]map[string]bool  `json:"roles"`
	Proposals    map[string]*Proposal        `json:"proposals"`
	Votes        map[string]map[string]*Vote `json:"votes"`
	Tallies      map[string]*TallyResult     `json:"tallies"`
	Executions   map[string]*ExecutionResult `json:"executions"`
	Seq          int                         `json:"seq"`
}
