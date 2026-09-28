package governance

import "time"

// validateActions 在提案创建时校验动作列表，拒绝无法执行的提案。
func validateActions(actions []Action) error {
	for i, a := range actions {
		switch a.Type {
		case ActionMint:
			if a.To == "" || a.Amount <= 0 {
				return wrapf(ErrInvalidArgument, "action[%d] mint requires non-empty To and positive amount", i)
			}
		case ActionBurn:
			if a.From == "" || a.Amount <= 0 {
				return wrapf(ErrInvalidArgument, "action[%d] burn requires non-empty From and positive amount", i)
			}
		case ActionTransfer:
			if a.From == "" || a.To == "" || a.From == a.To || a.Amount <= 0 {
				return wrapf(ErrInvalidArgument, "action[%d] transfer requires distinct non-empty From/To and positive amount", i)
			}
		case ActionGrantRole:
			if a.Role == "" || a.Member == "" {
				return wrapf(ErrInvalidArgument, "action[%d] grant_role requires non-empty Role and Member", i)
			}
		case ActionRevokeRole:
			if a.Role == "" || a.Member == "" {
				return wrapf(ErrInvalidArgument, "action[%d] revoke_role requires non-empty Role and Member", i)
			}
		default:
			return wrapf(ErrUnknownAction, "action[%d] has unknown type %q", i, a.Type)
		}
	}
	return nil
}

// undo 记录一个已应用动作的逆操作，用于失败回滚。
type undo struct {
	desc string
	run  func() error
}

// applyOne 在给定区块应用单个动作，返回其逆操作。
func applyOne(st *State, a Action, block int64) (undo, error) {
	switch a.Type {
	case ActionMint:
		if err := st.Ledger.Mint(a.To, a.Amount, block); err != nil {
			return undo{}, err
		}
		return undo{
			desc: "undo mint " + itoa(a.Amount) + " -> " + a.To,
			run:  func() error { return st.Ledger.Burn(a.To, a.Amount, block) },
		}, nil
	case ActionBurn:
		if err := st.Ledger.Burn(a.From, a.Amount, block); err != nil {
			return undo{}, err
		}
		return undo{
			desc: "undo burn " + a.From + " " + itoa(a.Amount),
			run:  func() error { return st.Ledger.Mint(a.From, a.Amount, block) },
		}, nil
	case ActionTransfer:
		if err := st.Ledger.Transfer(a.From, a.To, a.Amount, block); err != nil {
			return undo{}, err
		}
		return undo{
			desc: "undo transfer " + itoa(a.Amount) + " " + a.To + " -> " + a.From,
			run:  func() error { return st.Ledger.Transfer(a.To, a.From, a.Amount, block) },
		}, nil
	case ActionGrantRole:
		members := st.Roles[a.Role]
		already := members != nil && members[a.Member]
		if st.Roles[a.Role] == nil {
			st.Roles[a.Role] = map[string]bool{}
		}
		st.Roles[a.Role][a.Member] = true
		return undo{
			desc: "undo grant_role " + a.Role + ":" + a.Member,
			run: func() error {
				if !already {
					delete(st.Roles[a.Role], a.Member)
				}
				return nil
			},
		}, nil
	case ActionRevokeRole:
		members := st.Roles[a.Role]
		had := members != nil && members[a.Member]
		if members != nil {
			delete(members, a.Member)
		}
		return undo{
			desc: "undo revoke_role " + a.Role + ":" + a.Member,
			run: func() error {
				if had {
					if st.Roles[a.Role] == nil {
						st.Roles[a.Role] = map[string]bool{}
					}
					st.Roles[a.Role][a.Member] = true
				}
				return nil
			},
		}, nil
	default:
		return undo{}, wrapf(ErrUnknownAction, "action type %q", a.Type)
	}
}

// executeProposal 仅对计票成功的提案执行动作；已终态提案重复执行被拒绝。
func executeProposal(st *State, id string, block int64, now time.Time) (*ExecutionResult, error) {
	p, ok := st.Proposals[id]
	if !ok {
		return nil, wrapf(ErrNotFound, "proposal %s", id)
	}
	// 幂等闸门：只要已有执行结论（成功或已标记失败），重复执行一律拒绝。
	if prev, done := st.Executions[id]; done {
		return prev, wrapf(ErrAlreadyExecuted,
			"proposal %s is already finalized with status %s", id, prev.Status)
	}
	t, tallied := st.Tallies[id]
	if !tallied {
		return nil, wrapf(ErrProposalNotTallied, "proposal %s", id)
	}
	if t.Outcome != StatusSucceeded {
		if !t.QuorumMet {
			return nil, wrapf(ErrQuorumNotMet, "proposal %s", id)
		}
		return nil, wrapf(ErrThresholdNotMet, "proposal %s", id)
	}

	journal := make([]undo, 0, len(p.Actions))
	for i, a := range p.Actions {
		u, err := applyOne(st, a, block)
		if err != nil {
			// 部分失败：按相反顺序回滚此前已成功应用的动作。
			for j := len(journal) - 1; j >= 0; j-- {
				if rbErr := journal[j].run(); rbErr != nil {
					// 回滚失败属于严重情况：明确标记并让上层持久化，绝不静默。
					res := &ExecutionResult{
						ProposalID:  id,
						Status:      StatusExecutionFailed,
						Applied:     appliedActions(p.Actions, i),
						FailedIndex: i,
						Reason:      "action failed: " + err.Error() + "; rollback failed: " + rbErr.Error(),
						Block:       block,
						ExecutedAt:  now,
					}
					p.Status = StatusExecutionFailed
					st.Executions[id] = res
					return res, wrapf(ErrRollbackFailed, "proposal %s action[%d] failed (%v) and rollback failed (%v)",
						id, i, err, rbErr)
				}
			}
			res := &ExecutionResult{
				ProposalID:  id,
				Status:      StatusExecutionFailed,
				Applied:     appliedActions(p.Actions, i),
				FailedIndex: i,
				Reason: "action[" + itoa(int64(i)) + "] failed: " + err.Error() +
					"; all previously applied actions were rolled back",
				Block:      block,
				ExecutedAt: now,
			}
			p.Status = StatusExecutionFailed
			st.Executions[id] = res
			return res, wrapf(ErrExecutionFailed, "proposal %s rolled back after action[%d]: %v", id, i, err)
		}
		journal = append(journal, u)
	}

	res := &ExecutionResult{
		ProposalID:  id,
		Status:      StatusExecuted,
		Applied:     p.Actions,
		FailedIndex: -1,
		Block:       block,
		ExecutedAt:  now,
	}
	p.Status = StatusExecuted
	st.Executions[id] = res
	return res, nil
}

func appliedActions(all []Action, failedAt int) []Action {
	out := make([]Action, 0, failedAt)
	out = append(out, all[:failedAt]...)
	return out
}
