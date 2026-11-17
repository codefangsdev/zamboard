package orch

import (
	"strings"
	"testing"
	"time"

	"github.com/azamrizki/nextboard/internal/agentstate"
	"github.com/azamrizki/nextboard/internal/board"
	"github.com/azamrizki/nextboard/internal/delivery"
)

func executionPlanForTest(t *testing.T, s *Service, tid, commandID string) *ExecutionPlan {
	t.Helper()
	for _, m := range s.Threads.Last(tid, 100) {
		if m.ExecutionPlan != nil && m.ExecutionPlan.CommandID == commandID {
			return m.ExecutionPlan
		}
	}
	t.Fatal("execution plan missing")
	return nil
}

func TestExecutionPlanOrderLifecycleAndRestart(t *testing.T) {
	h, s := newFakeHub(t)
	p, wid := h.project("plan")
	ash := h.console(p, wid, "Ash", "claude", agentstate.Ready)
	r := cmd(t, s, wid, "@Ash run the tests")
	p1 := executionPlanForTest(t, s, r.ThreadID, r.CommandID)
	if len(p1.Steps) != 1 || p1.State != "running" || len(p1.Steps[0].OrderIDs) != 1 || p1.Steps[0].OrderIDs[0] != r.OrderID {
		t.Fatalf("plan does not track order: %+v", p1)
	}
	if p1.Steps[0].Assignees[0].Handle != "Ash" {
		t.Fatalf("assignee: %+v", p1.Steps[0])
	}
	d := h.deliveriesTo(ash.ID)[0]
	h.setDelivery(s, d.ID, delivery.Confirmed)
	if got := executionPlanForTest(t, s, r.ThreadID, r.CommandID).Steps[0].State; got != "working" {
		t.Fatalf("delivered state = %q", got)
	}
	// The serialized snapshot survives constructing a new service on the same scratch store.
	s.Orders.Save(t0)
	n := New(h, h.st)
	n.Now = s.Now
	n.Threads.Now = n.Now
	t.Cleanup(n.Close)
	if got := executionPlanForTest(t, n, r.ThreadID, r.CommandID); got.ID != p1.ID || got.Steps[0].State != "working" {
		t.Fatalf("reload lost progress: %+v", got)
	}
	n.Start()
	got := executionPlanForTest(t, n, r.ThreadID, r.CommandID)
	if got.State != "failed" || !strings.Contains(got.Steps[0].Error, "interrupted by hub restart") {
		t.Fatalf("restart did not reconcile interrupted plan: %+v", got)
	}
	if len(h.deliveriesTo(ash.ID)) != 1 {
		t.Fatal("restart sent work again automatically")
	}
	if _, err := n.ResendOrder(r.OrderID); err != ErrNothingToResend {
		// This order had been delivered before restart; it must not be sent twice.
		t.Fatalf("delivered order offered for resend: %v", err)
	}
}

func TestExecutionPlanConfirmCancelAndExpiry(t *testing.T) {
	for _, decision := range []string{"confirm", "cancel", "expire"} {
		t.Run(decision, func(t *testing.T) {
			h, s := newFakeHub(t)
			p, wid := h.project("approval")
			h.console(p, wid, "Ash", "claude", agentstate.Ready)
			r := cmd(t, s, wid, "restart Ash")
			if r.Confirmation == nil {
				t.Fatal("missing confirmation")
			}
			plan := executionPlanForTest(t, s, r.ThreadID, r.CommandID)
			if plan.State != "awaitingApproval" || plan.ConfirmationID != r.Confirmation.ID || len(h.calls) != 0 {
				t.Fatalf("work ran before approval: %+v", plan)
			}
			want := "finished"
			if decision == "expire" {
				s.Tick(t0.Add(61 * time.Second))
				want = "expired"
			} else {
				if _, err := s.Decide(r.Confirmation.ID, decision, "", "admin"); err != nil {
					t.Fatal(err)
				}
				if decision == "cancel" {
					want = "cancelled"
				}
			}
			if got := executionPlanForTest(t, s, r.ThreadID, r.CommandID); got.State != want || got.Steps[0].State != want {
				t.Fatalf("plan = %+v, want %s", got, want)
			}
			if decision != "confirm" && len(h.calls) != 0 {
				t.Fatal("unapproved work ran")
			}
		})
	}
}

func TestExecutionPlanDeliveryFailureAndThreadIsolation(t *testing.T) {
	h, s := newFakeHub(t)
	p, wid := h.project("failure")
	ash := h.console(p, wid, "Ash", "claude", agentstate.Ready)
	h.delErr[ash.ID] = delivery.ErrNotAgent
	r := cmd(t, s, wid, "@Ash run the tests")
	plan := executionPlanForTest(t, s, r.ThreadID, r.CommandID)
	if plan.State != "failed" || plan.Steps[0].Error == "" {
		t.Fatalf("failure lost: %+v", plan)
	}
	other, _ := h.project("other")
	if s.mutateExecutionPlan(other.ID, r.CommandID, func(p *ExecutionPlan) { p.State = "finished" }) {
		t.Fatal("plan found in another project thread")
	}
}

func TestExecutionPlanAssigneesFollowConfirmationChoice(t *testing.T) {
	h, s := newFakeHub(t)
	p, wid := h.project("choice")
	ash := h.console(p, wid, "Ash", "claude", agentstate.Ready)
	bea := h.console(p, wid, "Bea", "claude", agentstate.Ready)
	c := &execCtx{commandID: "choice-command", threadID: p.ID, projectID: p.ID, workspaceID: wid}
	cf := s.addConfirm(c, &Confirmation{Kind: ConfSendToAll, Steps: []Step{{Op: "send", Targets: []string{ash.ID, bea.ID}, Text: "Build"}}})
	if _, err := s.Decide(cf.ID, "confirm", ash.ID, "admin"); err != nil {
		t.Fatal(err)
	}
	plan := executionPlanForTest(t, s, p.ID, c.commandID)
	if len(h.deliveriesTo(ash.ID)) != 1 || len(h.deliveriesTo(bea.ID)) != 0 {
		t.Fatal("choice did not narrow delivery")
	}
	if len(plan.Steps[0].Assignees) != 1 || plan.Steps[0].Assignees[0].SessionID != ash.ID {
		t.Fatalf("timeline assignees differ from actual delivery: %+v", plan.Steps[0].Assignees)
	}
}

func TestExecutionPlanAggregateState(t *testing.T) {
	for _, tc := range []struct {
		name   string
		states []string
		want   string
	}{
		{"finished", []string{"finished", "finished"}, "finished"},
		{"cancelled", []string{"finished", "cancelled"}, "cancelled"},
		{"expired", []string{"expired"}, "expired"},
		{"failed", []string{"finished", "failed"}, "failed"},
		{"blocked", []string{"finished", "blocked"}, "blocked"},
		{"still running", []string{"failed", "working"}, "running"},
		{"approval", []string{"working", "awaitingApproval"}, "awaitingApproval"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := &ExecutionPlan{}
			for _, state := range tc.states {
				p.Steps = append(p.Steps, ExecutionPlanStep{State: state})
			}
			planState(p)
			if p.State != tc.want {
				t.Fatalf("state = %q, want %q", p.State, tc.want)
			}
		})
	}
}

func TestExecutionPlanTracksDeliveryStates(t *testing.T) {
	h, s := newFakeHub(t)
	p, wid := h.project("delivery states")
	h.console(p, wid, "Ash", "claude", agentstate.Ready)
	r := cmd(t, s, wid, "@Ash run the tests")
	o, _ := s.Orders.Get(r.OrderID)
	for _, state := range []string{TQueued, TDelivered, TWorking, TNeedsYou, TFinished} {
		o.Targets[0].State = state
		s.syncPlanOrder(o)
		if got := executionPlanForTest(t, s, r.ThreadID, r.CommandID).Steps[0].State; got != state {
			t.Fatalf("target %q shown as %q", state, got)
		}
	}
}

func TestPlanBoardWireOwnership(t *testing.T) {
	target := &board.Node{Kind: "planboard", Data: []byte(`{"orchestratorId":"lead-a"}`)}
	if !isWireTarget(target, &board.Node{ID: "lead-a", Kind: KindOrchestrator}) {
		t.Fatal("owner cannot connect to its Plan Board")
	}
	if isWireTarget(target, &board.Node{ID: "lead-b", Kind: KindOrchestrator}) {
		t.Fatal("another orchestrator can connect to this Plan Board")
	}
	if isWireTarget(target, &board.Node{ID: "lead-a", Kind: KindMemory}) {
		t.Fatal("non-orchestrator can own a Plan Board")
	}
}
