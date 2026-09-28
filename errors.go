package governance

import (
	"errors"
	"fmt"
)

// 可区分的失败原因：调用方可用 errors.Is 精确判定。
var (
	ErrNotImplemented = errors.New("not implemented")

	// 通用
	ErrNotFound        = errors.New("not found")
	ErrInvalidArgument = errors.New("invalid argument")

	// 投票相关
	ErrAlreadyVoted     = errors.New("duplicate vote rejected: voter has already voted")
	ErrNoVotingPower    = errors.New("voter has no effective voting power at the proposal snapshot")
	ErrInvalidOption    = errors.New("invalid vote option")
	ErrVotingNotStarted = errors.New("vote rejected: voting period has not started")
	ErrVotingClosed     = errors.New("vote rejected: voting period has ended")

	// 委托相关
	ErrSelfDelegation     = errors.New("self delegation is not allowed")
	ErrDelegationCycle    = errors.New("delegation rejected: delegation chain would contain a cycle")
	ErrDelegationNotFound = errors.New("no active delegation to revoke")

	// 快照相关
	ErrSnapshotInFuture = errors.New("snapshot block is in the future")
	ErrSnapshotEmpty    = errors.New("snapshot has zero total voting weight")

	// 计票 / 执行相关
	ErrVotingOngoing       = errors.New("voting period is still ongoing")
	ErrProposalNotTallied  = errors.New("proposal has not been tallied yet")
	ErrQuorumNotMet        = errors.New("proposal defeated: quorum not met")
	ErrThresholdNotMet     = errors.New("proposal defeated: approval threshold not met")
	ErrAlreadyExecuted     = errors.New("proposal already finalized: repeated execution rejected")
	ErrExecutionFailed     = errors.New("proposal execution failed and all applied actions were rolled back")
	ErrRollbackFailed      = errors.New("rollback after failed execution did not complete")
	ErrInsufficientBalance = errors.New("insufficient balance")
	ErrUnknownAction       = errors.New("unknown action type")
)

// wrapf 用动态信息包装哨兵错误，既保留 errors.Is 可判定性，又能给出具体上下文。
func wrapf(err error, format string, args ...any) error {
	return fmt.Errorf("%s: %w", fmt.Sprintf(format, args...), err)
}
