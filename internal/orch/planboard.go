package orch

import (
	"encoding/json"
	"fmt"
	"time"
)

// ExecutionPlan is a durable projection of execution, never a scheduling policy.
type ExecutionPlan struct {
	ID             string              `json:"id"`
	CommandID      string              `json:"commandId"`
	WorkspaceID    string              `json:"workspaceId"`
	CardID         string              `json:"cardId,omitempty"`
	State          string              `json:"state"`
	ConfirmationID string              `json:"confirmationId,omitempty"`
	UpdatedAt      time.Time           `json:"updatedAt"`
	Steps          []ExecutionPlanStep `json:"steps"`
}
type ExecutionPlanStep struct {
	ID        string         `json:"id"`
	Order     int            `json:"order"`
	Op        string         `json:"op"`
	Title     string         `json:"title"`
	Assignees []PlanAssignee `json:"assignees"`
	DependsOn []string       `json:"dependsOn"`
	State     string         `json:"state"`
	OrderIDs  []string       `json:"orderIds"`
	Result    string         `json:"result,omitempty"`
	Error     string         `json:"error,omitempty"`
}
type PlanAssignee struct {
	SessionID string `json:"sessionId"`
	Handle    string `json:"handle"`
}

// mutateExecutionPlan serializes read/modify/write and clones nested values so
// readers and published event snapshots cannot observe later mutations.
func (s *Service) mutateExecutionPlan(tid, commandID string, f func(*ExecutionPlan)) bool {
	s.planBoardMu.Lock()
	defer s.planBoardMu.Unlock()
	s.Threads.mu.Lock()
	var m ThreadMessage
	for _, item := range s.Threads.loadLocked(tid).msgs {
		if item.ExecutionPlan != nil && item.ExecutionPlan.CommandID == commandID {
			raw, _ := json.Marshal(item)
			_ = json.Unmarshal(raw, &m)
			break
		}
	}
	s.Threads.mu.Unlock()
	if m.ExecutionPlan == nil {
		return false
	}
	f(m.ExecutionPlan)
	m.ExecutionPlan.UpdatedAt = s.Now().UTC()
	s.Threads.Update(m)
	return true
}
func (s *Service) planAssignees(targets []string) []PlanAssignee {
	assignees := []PlanAssignee{}
	for _, sid := range uniq(targets) {
		in, _ := s.hub.SessionInfo(sid)
		assignees = append(assignees, PlanAssignee{sid, in.Handle})
	}
	return assignees
}

func (s *Service) ensureExecutionPlan(c *execCtx, steps []Step) {
	if len(steps) == 0 {
		return
	}
	if s.mutateExecutionPlan(c.threadID, c.commandID, func(p *ExecutionPlan) {
		// A confirmation can narrow the proposed recipients before execution.
		for i, st := range steps {
			if i < len(p.Steps) {
				p.Steps[i].Assignees = s.planAssignees(st.Targets)
			}
		}
	}) {
		return
	}
	p := &ExecutionPlan{ID: c.commandID, CommandID: c.commandID, WorkspaceID: c.workspaceID, CardID: c.cardID, State: "planned", UpdatedAt: s.Now().UTC(), Steps: []ExecutionPlanStep{}}
	for i, st := range steps {
		x := ExecutionPlanStep{ID: fmt.Sprintf("%s:%d", c.commandID, i+1), Order: i + 1, Op: st.Op, Title: firstNonEmpty(st.Text, st.Op), State: "pending", Assignees: []PlanAssignee{}, DependsOn: []string{}, OrderIDs: []string{}}
		x.Assignees = s.planAssignees(st.Targets)
		p.Steps = append(p.Steps, x)
	}
	s.say(c, ThreadMessage{Kind: "executionPlan", Text: "Execution plan", ExecutionPlan: p})
}
func planState(p *ExecutionPlan) {
	p.State = "finished"
	failed, active, waiting, cancelled, expired, blocked := false, false, false, false, false, false
	for _, st := range p.Steps {
		failed = failed || st.State == "failed"
		active = active || st.State == "pending" || st.State == "running" || st.State == "queued" || st.State == "delivered" || st.State == "working"
		waiting = waiting || st.State == "needsYou" || st.State == "awaitingApproval"
		cancelled = cancelled || st.State == "cancelled"
		expired = expired || st.State == "expired"
		blocked = blocked || st.State == "blocked"
	}
	if cancelled {
		p.State = "cancelled"
	}
	if expired {
		p.State = "expired"
	}
	if failed {
		p.State = "failed"
	}
	if blocked {
		p.State = "blocked"
	}
	if active {
		p.State = "running"
	}
	if waiting {
		p.State = "awaitingApproval"
	}
}
func (s *Service) updatePlanStep(c *execCtx, state, problem string) {
	s.mutateExecutionPlan(c.threadID, c.commandID, func(p *ExecutionPlan) {
		for i := range p.Steps {
			st := &p.Steps[i]
			if st.ID != c.planStepID {
				continue
			}
			if len(st.OrderIDs) > 0 && state == "finished" {
				continue
			}
			st.State, st.Error = state, problem
			if state == "finished" {
				st.Result = "Completed"
			}
		}
		planState(p)
	})
}
func (s *Service) linkPlanOrder(c *execCtx, o *Order) {
	if c.planStepID == "" {
		return
	}
	o.PlanStepID = c.planStepID
	s.mutateExecutionPlan(c.threadID, c.commandID, func(p *ExecutionPlan) {
		for i := range p.Steps {
			if p.Steps[i].ID == c.planStepID {
				p.Steps[i].OrderIDs = append(p.Steps[i].OrderIDs, o.ID)
			}
		}
	})
}
func (s *Service) syncPlanOrder(o Order) {
	if o.PlanStepID == "" {
		return
	}
	s.mutateExecutionPlan(o.ThreadID, o.CommandID, func(p *ExecutionPlan) {
		for i := range p.Steps {
			st := &p.Steps[i]
			if st.ID != o.PlanStepID {
				continue
			}
			st.State = "finished"
			st.Error = ""
			st.Result = ""
			active, waiting, failed, cancelled := false, false, false, false
			activeState := TQueued
			for _, t := range o.Targets {
				active = active || !TargetFinal(t.State)
				if t.State == TWorking || (t.State == TDelivered && activeState != TWorking) {
					activeState = t.State
				}
				waiting = waiting || t.State == TNeedsYou
				failed = failed || t.State == TFailed || t.State == TTimeout
				cancelled = cancelled || t.State == TCancelled
				if t.Error != "" {
					st.Error = t.Error
				}
			}
			if cancelled {
				st.State = "cancelled"
			}
			if failed {
				st.State = "failed"
			}
			if active {
				st.State = activeState
			}
			if waiting {
				st.State = "needsYou"
			}
			if st.State == "finished" {
				st.Result = "Agent work finished"
			}
		}
		planState(p)
	})
}
func (s *Service) syncPlanConfirmation(cf Confirmation) {
	s.mutateExecutionPlan(cf.ThreadID, cf.CommandID, func(p *ExecutionPlan) {
		p.ConfirmationID = cf.ID
		switch cf.State {
		case ConfPending:
			p.State = "awaitingApproval"
		case ConfCancelled, ConfExpired:
			p.State = cf.State
		default:
			return
		}
		for i := range p.Steps {
			if cf.State == ConfPending {
				p.Steps[i].State = "awaitingApproval"
			} else {
				p.Steps[i].State = cf.State
			}
		}
	})
}
