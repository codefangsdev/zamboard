import type { ExecutionPlan } from './types';

const LABELS: Record<string, string> = {
  planned: 'Planned', pending: 'Pending', running: 'Running', queued: 'Queued',
  delivered: 'Delivered', working: 'Working', needsYou: 'Needs you',
  awaitingApproval: 'Awaiting approval', finished: 'Finished', failed: 'Failed',
  cancelled: 'Cancelled', expired: 'Expired', blocked: 'Blocked', skipped: 'Skipped', timeout: 'No reply',
};

export function planStateLabel(state: string): string {
  return LABELS[state] ?? state;
}

/** A projection of execution. Approval continues through the existing confirmation card. */
export function PlanTimeline({ plan, todo = false }: { plan: ExecutionPlan; todo?: boolean }) {
  const steps = [...(plan.steps ?? [])].sort((a, b) => a.order - b.order);
  const byId = new Map(steps.map((step) => [step.id, step]));
  return (
    <div className={`execution-plan${todo ? ' execution-todo' : ''}`}>
      <span className="report-state">{todo && `${steps.filter((s) => s.state === 'finished').length}/${steps.length} done · `}{planStateLabel(plan.state)}</span>
      <ol className="execution-steps" aria-label={todo ? 'Plan tasks' : 'Execution steps'}>
        {steps.map((step) => (
          <li key={step.id} className={`execution-step state-${step.state}`}>
            <div className="execution-step-head">
              {todo && <input type="checkbox" checked={step.state === 'finished'} disabled aria-label={`${step.title || step.op}: ${planStateLabel(step.state)}`} />}
              <strong>{!todo && `${step.order}. `}{step.title || step.op}</strong>
              <span className="execution-step-state">{planStateLabel(step.state)}</span>
            </div>
            {!!step.assignees?.length && (
              <div className="plan-text">{step.assignees.map((a) => a.handle ? `@${a.handle}` : a.agent || 'Unassigned agent').join(', ')}</div>
            )}
            {!!step.dependsOn?.length && (
              <div className="report-state">Depends on: {step.dependsOn.map((id) => {
                const dependency = byId.get(id);
                return dependency ? `${dependency.order}. ${dependency.title || dependency.op}` : `Unknown step (${id})`;
              }).join(', ')}</div>
            )}
            {step.result && <p className="plan-text">{step.result}</p>}
            {step.error && <p className="execution-step-error">{step.error}</p>}
          </li>
        ))}
      </ol>
    </div>
  );
}
