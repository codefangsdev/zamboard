import { createElement } from 'react';
import { renderToStaticMarkup } from 'react-dom/server';
import { describe, expect, it, vi } from 'vitest';
import type { ThreadMessage } from '../../orch/types';
import type { FlowNode } from '../flow';
import { PlanBoardTile } from './PlanBoardTile';

const state = vi.hoisted(() => ({ messages: [] as ThreadMessage[], threadId: '' }));
vi.mock('../ui', () => ({ useBoardEnv: () => ({ ctl: { node: (id: string) => ({ data: { title: id === 'lead' ? 'Lead A' : 'Lead B' } }) } }) }));
vi.mock('@xyflow/react', () => ({ Handle: () => null, Position: { Left: 'left' } }));
vi.mock('../../orch/store', () => ({
  orchStore: { loadThread: vi.fn() },
  useThread: (id: string) => { state.threadId = id; return { status: 'ready', error: '', messages: state.messages }; },
}));
vi.mock('./common', () => ({
  TileFrame: ({ children }: { children: React.ReactNode }) => children,
  TileHead: ({ title }: { title: string }) => title,
}));

const render = () => renderToStaticMarkup(createElement(PlanBoardTile, {
  id: 'board', data: { node: { id: 'board', kind: 'planboard', x: 0, y: 0, w: 480, h: 560, data: { orchestratorId: 'lead' } } },
} as React.ComponentProps<typeof PlanBoardTile> & { data: FlowNode['data'] }));

describe('Plan Board card', () => {
  it('reads its linked orchestrator and explains when no execution plan exists', () => {
    state.messages = [];
    expect(render()).toContain('Send a new task');
    expect(render()).toContain('Plan Board · Lead A');
    expect(state.threadId).toBe('lead');
  });
  it('shows persisted execution progress and ignores ordinary chat', () => {
    state.messages = [
      { id: 'chat', threadId: 'lead', at: '', role: 'hub', kind: 'reply', text: 'ordinary reply' },
      { id: 'plan', threadId: 'lead', at: '', role: 'hub', kind: 'executionPlan', text: '', executionPlan: {
        id: 'p', commandId: 'c', state: 'running', steps: [{ id: 'step', order: 1, op: 'send', title: 'Check project', state: 'working' }],
      } },
    ];
    const html = render();
    expect(html).toContain('Check project');
    expect(html).toContain('Working');
    expect(html).toContain('type="checkbox"');
    expect(html).toContain('0/1 done');
    expect(html).not.toContain('checked=""');
    expect(html).not.toContain('ordinary reply');
    expect(html).not.toContain('No execution plan yet');
    state.messages[1].executionPlan!.steps![0].state = 'finished';
    const completed = render();
    expect(completed).toContain('checked=""');
    expect(completed).toContain('1/1 done');
  });
  it('separates each plan into a named batch with its own todo progress', () => {
    state.messages = ['First task', 'Second task'].map((title, i) => ({
      id: `plan-${i}`, threadId: 'lead', at: '2026-11-17T03:00:00Z', role: 'hub', kind: 'executionPlan', text: '',
      executionPlan: { id: `p-${i}`, commandId: `c-${i}`, state: i ? 'working' : 'finished',
        steps: [{ id: `step-${i}`, order: 1, op: 'send', title, state: i ? 'working' : 'finished' }] },
    }));
    const html = render();
    expect(html).toMatch(/aria-label="Plan batch 1"[^]*First task[^]*<\/section>/);
    expect(html).toMatch(/aria-label="Plan batch 2"[^]*Second task[^]*<\/section>/);
    expect(html.match(/class="planboard-batch"/g)).toHaveLength(2);
    expect(html).toContain('1/1 done');
    expect(html).toContain('0/1 done');
  });
});
