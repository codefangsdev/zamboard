import { memo, useEffect } from 'react';
import { Handle, Position, type NodeProps } from '@xyflow/react';
import { PlanTimeline } from '../../orch/PlanTimeline';
import { orchStore, useThread } from '../../orch/store';
import type { FlowNode } from '../flow';
import { useBoardEnv } from '../ui';
import { TileFrame, TileHead } from './common';

function PlanBoardTileImpl({ id, data, selected }: NodeProps<FlowNode>) {
  const { ctl } = useBoardEnv();
  const threadId = typeof data.node.data.orchestratorId === 'string' ? data.node.data.orchestratorId : null;
  const owner = threadId ? ctl.node(threadId) : undefined;
  const ownerName = typeof owner?.data.title === 'string' ? owner.data.title : 'Orchestrator';
  const thread = useThread(threadId);
  useEffect(() => { if (threadId) void orchStore.loadThread(threadId); }, [threadId]);
  const plans = thread.messages.filter((m) => m.executionPlan);
  return (
    <TileFrame id={id} kind="planboard" selected={!!selected} label="Plan Board" minW={300} minH={240}>
      <Handle type="target" position={Position.Left} id="in" className="nb-handle nb-handle-in" isConnectable={false} />
      <TileHead id={id} icon="workflow" title={`Plan Board · ${ownerName}`} />
      <div className="planboard-body nodrag nowheel">
        {!threadId && <p role="alert">This Plan Board has no linked orchestrator.</p>}
        {thread.status === 'loading' && !plans.length && <p role="status">Loading plans…</p>}
        {thread.error && <p role="alert">{thread.error}</p>}
        {thread.status === 'ready' && !plans.length && <p>No execution plan yet. Send a new task from the linked orchestrator.</p>}
        {plans.map((m, i) => (
          <section key={m.id} className="planboard-batch" aria-label={`Plan batch ${i + 1}`}>
            <header className="planboard-batch-head">
              <strong>Batch {i + 1}</strong>
              {m.at && <time dateTime={m.at}>{new Date(m.at).toLocaleString()}</time>}
            </header>
            <PlanTimeline plan={m.executionPlan!} todo />
          </section>
        ))}
      </div>
    </TileFrame>
  );
}

export const PlanBoardTile = memo(PlanBoardTileImpl);
