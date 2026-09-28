package governance

// Error code 常量：调用方可用 errors.Code(err) 区分失败原因。
const (
	CodeUnknown             = "unknown"
	CodeNotFound            = "not_found"
	CodeAlreadyExists       = "already_exists"
	CodeInvalidOption       = "invalid_option"
	CodeDoubleVote          = "double_vote"
	CodeVotingClosed        = "voting_closed"
	CodeVotingNotStarted    = "voting_not_started"
	CodeNoVotingPower       = "no_voting_power"
	CodeDelegationCycle     = "delegation_cycle"
	CodeSelfDelegation      = "self_delegation"
	CodeInsufficientBalance = "insufficient_balance"
	CodeProposalNotActive   = "proposal_not_active"
	CodeAlreadyFinalized    = "already_finalized"
	CodeVotingStillOpen     = "voting_still_open"
	CodeQuorumNotMet        = "quorum_not_met"
	CodeThresholdNotMet     = "threshold_not_met"
	CodeNotExecutable       = "not_executable"
	CodeAlreadyExecuted     = "already_executed"
	CodeExecutionFailed     = "execution_failed"
	CodeSnapshotUnavailable = "snapshot_unavailable"
	CodeInvalidArgument     = "invalid_argument"
)

// GovError 是携带稳定错误码的治理错误，测试与调用方可按 Code 区分原因。
type GovError struct {
	Code   string
	Reason string
}

func (e *GovError) Error() string {
	return "governance error [" + e.Code + "]: " + e.Reason
}

func newError(code, reason string) *GovError {
	return &GovError{Code: code, Reason: reason}
}
