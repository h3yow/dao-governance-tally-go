package governance

import (
	"errors"
	"fmt"
	"sync"
	"testing"
)

// 并发场景：同一投票者用多个 goroutine 重复投票，只能有一张票生效；
// 不同投票者并发投票，每张都必须被统计，不得丢失。
func TestConcurrentVotingNoDoubleCountNoLoss(t *testing.T) {
	g, _ := newTestGovernor(t)
	mustAdvance(t, g, 1)
	const voters = 40
	for i := 0; i < voters; i++ {
		name := fmt.Sprintf("voter%02d", i)
		if err := g.Mint(name, 1); err != nil {
			t.Fatal(err)
		}
	}
	p, err := g.CreateProposal(CreateProposalInput{
		Title: "concurrent votes", Proposer: "voter00",
		SnapshotBlock: 1, VoteStartBlock: 2, VoteEndBlock: 100,
		Quorum: 0, Threshold: 0.5,
		Actions: []Action{{Type: ActionMint, To: "x", Amount: 1}},
	})
	if err != nil {
		t.Fatal(err)
	}
	mustAdvance(t, g, 2)

	var wg sync.WaitGroup
	var accepted, duplicated int64
	var mu sync.Mutex

	// 每个投票者发起 3 次并发投票：预期恰好 1 次 accepted，2 次 ErrAlreadyVoted。
	for i := 0; i < voters; i++ {
		name := fmt.Sprintf("voter%02d", i)
		for k := 0; k < 3; k++ {
			wg.Add(1)
			go func(voter string) {
				defer wg.Done()
				_, err := g.Vote(p.ID, voter, OptionYes)
				mu.Lock()
				defer mu.Unlock()
				switch {
				case err == nil:
					accepted++
				case errors.Is(err, ErrAlreadyVoted):
					duplicated++
				default:
					t.Errorf("unexpected vote error for %s: %v", voter, err)
				}
			}(name)
		}
	}
	wg.Wait()
	t.Logf("concurrent votes: accepted=%d duplicated=%d (expect %d/%d)",
		accepted, duplicated, voters, voters*2)
	if accepted != voters {
		t.Fatalf("accepted votes = %d, want %d (vote loss or double count)", accepted, voters)
	}
	if duplicated != voters*2 {
		t.Fatalf("duplicated rejections = %d, want %d", duplicated, voters*2)
	}

	mustAdvance(t, g, 101)
	r, err := g.Tally(p.ID)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("tally after concurrent voting: yes=%d participating=%d/%d",
		r.Yes, r.ParticipatingWeight, r.TotalWeight)
	if r.Yes != voters {
		t.Fatalf("tally yes=%d, want %d (double count or loss)", r.Yes, voters)
	}
}

// 并发执行：多个 goroutine 同时 Execute，只能有一个真正执行，
// 其余全部拿到 ErrAlreadyExecuted；链上效果只发生一次。
func TestConcurrentExecutionHappensOnce(t *testing.T) {
	g, _ := newTestGovernor(t)
	mustAdvance(t, g, 1)
	if err := g.Mint("alice", 100); err != nil {
		t.Fatal(err)
	}
	p, err := g.CreateProposal(CreateProposalInput{
		Title: "concurrent exec", Proposer: "alice",
		SnapshotBlock: 1, VoteStartBlock: 2, VoteEndBlock: 3,
		Quorum: 0, Threshold: 0.5,
		Actions: []Action{{Type: ActionMint, To: "treasury", Amount: 7}},
	})
	if err != nil {
		t.Fatal(err)
	}
	mustAdvance(t, g, 2)
	if _, err := g.Vote(p.ID, "alice", OptionYes); err != nil {
		t.Fatal(err)
	}
	mustAdvance(t, g, 4)
	if _, err := g.Tally(p.ID); err != nil {
		t.Fatal(err)
	}

	const n = 20
	var wg sync.WaitGroup
	var success, rejected int64
	var mu sync.Mutex
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := g.Execute(p.ID)
			mu.Lock()
			defer mu.Unlock()
			switch {
			case err == nil:
				success++
			case errors.Is(err, ErrAlreadyExecuted):
				rejected++
			default:
				t.Errorf("unexpected execute error: %v", err)
			}
		}()
	}
	wg.Wait()

	treasury, _ := g.Balance("treasury")
	t.Logf("concurrent execute: success=%d rejected=%d treasury=%d (expect 1/%d/7)",
		success, rejected, treasury, n-1)
	if success != 1 || rejected != n-1 {
		t.Fatalf("execution must happen exactly once: success=%d rejected=%d", success, rejected)
	}
	if treasury != 7 {
		t.Fatalf("mint amount applied more than once or lost: treasury=%d", treasury)
	}
}

// 并发计票：结论只计算一次，重复调用返回同一持久化结果。
func TestConcurrentTallyHappensOnce(t *testing.T) {
	g, _ := newTestGovernor(t)
	mustAdvance(t, g, 1)
	if err := g.Mint("alice", 10); err != nil {
		t.Fatal(err)
	}
	if err := g.Mint("bob", 10); err != nil {
		t.Fatal(err)
	}
	p, err := g.CreateProposal(CreateProposalInput{
		Title: "concurrent tally", Proposer: "alice",
		SnapshotBlock: 1, VoteStartBlock: 2, VoteEndBlock: 3,
		Quorum: 0, Threshold: 0.6,
		Actions: []Action{{Type: ActionMint, To: "x", Amount: 1}},
	})
	if err != nil {
		t.Fatal(err)
	}
	mustAdvance(t, g, 2)
	if _, err := g.Vote(p.ID, "alice", OptionYes); err != nil {
		t.Fatal(err)
	}
	if _, err := g.Vote(p.ID, "bob", OptionNo); err != nil {
		t.Fatal(err)
	}
	mustAdvance(t, g, 4)

	const n = 16
	var wg sync.WaitGroup
	results := make([]*TallyResult, n)
	errs := make([]error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			results[i], errs[i] = g.Tally(p.ID)
		}(i)
	}
	wg.Wait()
	for _, e := range errs {
		if e != nil {
			t.Fatalf("concurrent tally error: %v", e)
		}
	}
	first := results[0]
	for i, r := range results {
		if r != first {
			t.Fatalf("tally %d returned a different result instance", i)
		}
	}
	t.Logf("concurrent tally converged on one persisted result: outcome=%s yes=%d no=%d",
		first.Outcome, first.Yes, first.No)
}
