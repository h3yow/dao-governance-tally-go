package governance

// Execute 执行已通过提案的动作。
// 规则：
//   - 只有 succeeded / execution_failed（上次失败已回滚，允许重试）可执行；
//   - executed 重复执行返回 CodeAlreadyExecuted；
//   - 全部动作先在内存中顺序应用，全部成功后才追加区块并一次性落盘；
//   - 任一动作失败：已应用动作按逆序补偿回滚，提案标记 execution_failed
//     并记录每步结果，状态不留半成品；
//   - 由单一互斥锁保护，并发执行不会重复应用或丢失。
func (g *Governor) Execute(proposalID, caller string) (*Proposal, error) {
	if caller == "" {
		return nil, newError(CodeInvalidArgument, "caller required")
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	st := g.store.State()
	p, ok := st.Proposals[proposalID]
	if !ok {
		return nil, newError(CodeNotFound, "proposal not found: "+proposalID)
	}
	switch p.Status {
	case StatusSucceeded, StatusExecutionFailed:
		// 允许执行 / 重试
	case StatusExecuted:
		return cloneProposal(p), newError(CodeAlreadyExecuted, "proposal already executed")
	default:
		return cloneProposal(p), newError(CodeNotExecutable, "proposal is not executable: "+string(p.Status))
	}

	applied := make([]AppliedAction, 0, len(p.Actions))
	var failErr error
	for _, act := range p.Actions {
		a := AppliedAction{Action: act}
		if err := applyAction(st, act, &a); err != nil {
			a.Applied = false
			a.ErrorMessage = err.Error()
			applied = append(applied, a)
			failErr = err
			break
		}
		a.Applied = true
		applied = append(applied, a)
	}

	if failErr != nil {
		// 逆向补偿
		for i := len(applied) - 1; i >= 0; i-- {
			if !applied[i].Applied {
				continue
			}
			if uerr := rollbackAction(st, applied[i]); uerr != nil {
				applied[i].ErrorMessage += "; rollback error: " + uerr.Error()
			} else {
				applied[i].Undone = true
			}
		}
		p.AppliedActions = applied
		p.Status = StatusExecutionFailed
		p.ExecutionError = failErr.Error()
		if serr := g.store.Save(); serr != nil {
			return nil, serr
		}
		return cloneProposal(p), newError(CodeExecutionFailed, "execution failed and rolled back: "+failErr.Error())
	}

	p.AppliedActions = applied
	p.Status = StatusExecuted
	p.ExecutionError = ""
	p.ExecutedAt = st.Height + 1
	if err := g.commitBlock(); err != nil {
		return nil, err
	}
	return cloneProposal(p), nil
}

// applyAction 在内存中应用单个动作，不持久化；失败返回错误，调用方负责回滚。
func applyAction(st *State, act Action, a *AppliedAction) error {
	switch act.Kind {
	case ActionMint:
		if act.To == "" || act.Amount <= 0 {
			return newError(CodeInvalidArgument, "invalid mint action")
		}
		st.Balances[act.To] += act.Amount
	case ActionTransfer:
		if act.From == "" || act.To == "" || act.Amount <= 0 {
			return newError(CodeInvalidArgument, "invalid transfer action")
		}
		if st.Balances[act.From] < act.Amount {
			return newError(CodeInsufficientBalance, "execution transfer: insufficient balance")
		}
		st.Balances[act.From] -= act.Amount
		st.Balances[act.To] += act.Amount
	case ActionSetQuorum:
		if act.NewValue < 0 || act.NewValue > 1 {
			return newError(CodeInvalidArgument, "quorum must be within [0,1]")
		}
		a.OldValue = st.DefaultQuorum
		st.DefaultQuorum = act.NewValue
	default:
		return newError(CodeInvalidArgument, "unknown action kind: "+string(act.Kind))
	}
	return nil
}

// rollbackAction 逆向补偿已应用的动作。
func rollbackAction(st *State, a AppliedAction) error {
	switch a.Action.Kind {
	case ActionMint:
		st.Balances[a.Action.To] -= a.Action.Amount
	case ActionTransfer:
		st.Balances[a.Action.To] -= a.Action.Amount
		st.Balances[a.Action.From] += a.Action.Amount
	case ActionSetQuorum:
		st.DefaultQuorum = a.OldValue
	}
	return nil
}
