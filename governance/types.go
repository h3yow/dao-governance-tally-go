package governance

// Address 是本地模拟的账户标识。
type Address string

// Block 表示一个本地区块，每笔改变余额/委托的写操作都会产生一个区块。
// 区块保存的是该操作执行后的状态拷贝，历史区块永不修改，是快照可解释性的来源。
type Block struct {
	Height    int64
	Balances  map[Address]int64
	Delegates map[Address]Address
}

// Snapshot 是某个区块高度上的投票权重快照（已展开委托链），创建后不可变。
// 之后的铸币、转账、销毁、委托/撤销都不会影响本快照，旧提案结论因此保持稳定。
type Snapshot struct {
	BlockHeight int64
	Weights     map[Address]int64
	TotalWeight int64
}

// VoteChoice 是投票选项。
type VoteChoice string

const (
	VoteYes     VoteChoice = "yes"
	VoteNo      VoteChoice = "no"
	VoteAbstain VoteChoice = "abstain"
)

func validChoice(c VoteChoice) bool {
	return c == VoteYes || c == VoteNo || c == VoteAbstain
}

// ProposalStatus 提案状态。
type ProposalStatus string

const (
	StatusActive          ProposalStatus = "active"
	StatusSucceeded       ProposalStatus = "succeeded" // 计票通过，等待执行
	StatusDefeated        ProposalStatus = "defeated"  // 计票未通过或人数不足
	StatusExecuted        ProposalStatus = "executed"
	StatusExecutionFailed ProposalStatus = "execution_failed" // 已回滚，可重试执行
)

// ActionKind 执行动作类型。
type ActionKind string

const (
	ActionMint      ActionKind = "mint"
	ActionTransfer  ActionKind = "transfer"
	ActionSetQuorum ActionKind = "set_quorum"
)

// Action 是提案通过后要顺序执行的原子动作之一。
type Action struct {
	Kind     ActionKind
	From     Address
	To       Address
	Amount   int64
	NewValue float64
}

// AppliedAction 记录动作的执行结果，用于部分失败时逆向补偿。
type AppliedAction struct {
	Action       Action
	Applied      bool
	Undone       bool
	OldValue     float64 // set_quorum 回滚用的旧值
	ErrorMessage string
}

// Proposal 是治理提案。
type Proposal struct {
	ID             string
	Title          string
	Creator        Address
	CreatedHeight  int64
	Snapshot       Snapshot
	StartHeight    int64
	EndHeight      int64
	Quorum         float64 // 法定人数门槛：参与权重 / 快照总权重
	Threshold      float64 // 通过门槛：赞成 / (赞成 + 反对)
	Actions        []Action
	Status         ProposalStatus
	Votes          map[Address]VoteChoice
	VoteHeights    map[Address]int64
	Tally          TallyResult
	AppliedActions []AppliedAction
	ExecutedAt     int64
	ExecutionError string
}

// TallyResult 是计票结论及其判定依据。
type TallyResult struct {
	YesWeight      int64
	NoWeight       int64
	AbstainWeight  int64
	TurnoutWeight  int64 // 参与权重 = 赞成 + 反对 + 弃权
	EligibleWeight int64 // 快照总权重
	TurnoutRatio   float64
	ApprovalRatio  float64 // 赞成 / (赞成 + 反对)；弃权不计入分母
	QuorumMet      bool
	ThresholdMet   bool
}

// State 是全部需要持久化的本地状态。
type State struct {
	Height           int64
	Blocks           []Block // 创世块在下标 0，历史只追加
	Balances         map[Address]int64
	Delegates        map[Address]Address
	NextProposalSeq  int64
	Proposals        map[string]*Proposal
	Treasury         int64
	DefaultQuorum    float64
	DefaultThreshold float64
}

func newState() *State {
	st := &State{
		Height:           0,
		Blocks:           []Block{{Height: 0, Balances: map[Address]int64{}, Delegates: map[Address]Address{}}},
		Balances:         map[Address]int64{},
		Delegates:        map[Address]Address{},
		Proposals:        map[string]*Proposal{},
		DefaultQuorum:    0.5,
		DefaultThreshold: 0.5,
	}
	return st
}

func (s *State) headBlock() Block {
	return s.Blocks[len(s.Blocks)-1]
}
