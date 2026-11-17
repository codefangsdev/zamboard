import { createElement } from 'react';
import { renderToStaticMarkup } from 'react-dom/server';
import { describe, expect, it } from 'vitest';
import { PlanTimeline } from './PlanTimeline';
import { mergeMessages } from './store';
import { messageText, replyDraftOf } from './lead';
import type { ExecutionPlan, ThreadMessage } from './types';

const plan: ExecutionPlan = {
  id: 'p', commandId: 'c', state: 'running', updatedAt: '2026-11-17T10:00:02Z',
  steps: [
    { id: 'b', order: 2, op: 'send', title: 'Review', state: 'blocked', dependsOn: ['a', 'missing'], assignees: [{}], error: 'Waiting for build' },
    { id: 'a', order: 1, op: 'send', title: 'Build <app>', state: 'finished', assignees: [{ handle: 'Ash' }], result: 'Built' },
  ],
};
const message: ThreadMessage = { id: 'm', threadId: 't', at: '2026-11-17T10:00:00Z', role: 'hub', kind: 'executionPlan', text: 'Execution plan', executionPlan: plan };

describe('execution timeline', () => {
  it('orders steps, resolves dependencies, escapes text and handles absent agents', () => {
    const html = renderToStaticMarkup(createElement(PlanTimeline, { plan }));
    expect(html.indexOf('1. Build')).toBeLessThan(html.indexOf('2. Review'));
    expect(html).toContain('Build &lt;app&gt;');
    expect(html).toContain('Depends on: 1. Build');
    expect(html).toContain('Unknown step (missing)');
    expect(html).toContain('Unassigned agent');
    expect(html).not.toContain('@undefined');
    expect(html).toContain('Waiting for build');
    expect(plan.steps?.[0].id).toBe('b');
  });

  it('preserves a newer live snapshot during stale REST hydration and accepts newer progress', () => {
    const old = { ...message, executionPlan: { ...plan, state: 'planned', updatedAt: '2026-11-17T10:00:01Z' } };
    expect(mergeMessages([message], [old])[0]).toBe(message);
    expect(mergeMessages([message], [{ ...old, executionPlan: undefined }])[0]).toBe(message);
    const newer = { ...message, executionPlan: { ...plan, state: 'finished', updatedAt: '2026-11-17T10:00:03Z' } };
    expect(mergeMessages([message], [newer])[0]).toBe(newer);
  });

  it('includes execution details for copying, reuse and reply excerpts; retains legacy notes', () => {
    expect(messageText(message)).toContain('Review [blocked] — Waiting for build');
    expect(replyDraftOf(message, 'Lead').excerpt).toContain('Build <app>');
    expect(messageText({ ...message, executionPlan: undefined, plan: [{ handle: 'Ash', text: 'Build' }] })).toBe('@Ash: Build');
  });

  it('renders terminal and approval states distinctly and tolerates empty snapshots', () => {
    for (const state of ['awaitingApproval', 'failed', 'cancelled', 'expired', 'blocked']) {
      const html = renderToStaticMarkup(createElement(PlanTimeline, { plan: { ...plan, state, steps: [{ ...plan.steps![0], state }] } }));
      expect(html).toContain(`state-${state}`);
    }
    expect(renderToStaticMarkup(createElement(PlanTimeline, { plan: { ...plan, steps: null } }))).toContain('Execution steps');
  });

  it('does not regress snapshots updated within the same millisecond', () => {
    const live = { ...message, executionPlan: { ...plan, state: 'finished', updatedAt: '2026-11-17T10:00:02.123900Z' } };
    const stale = { ...message, executionPlan: { ...plan, state: 'running', updatedAt: '2026-11-17T10:00:02.123100Z' } };
    expect(mergeMessages([live], [stale])[0]).toBe(live);
  });
});
