import { memo, useEffect, useState } from 'react';
import type { NodeProps } from '@xyflow/react';
import { apiRequest, errorMessage } from '../../api';
import { Button } from '../../components/Button';
import { Modal } from '../../components/Modal';
import { SelectField, TextField } from '../../components/form';
import { useSessionList } from '../../orch/hooks';
import { isAgentSession } from '../../orch/actions';
import type { FlowNode } from '../flow';
import { TileFrame, TileHead } from './common';

function BrowserTestTileImpl({ id, data, selected }: NodeProps<FlowNode>) {
  const sid = typeof data.node.data.sessionId === 'string' ? data.node.data.sessionId : '';
  const [frame, setFrame] = useState('');
  const [error, setError] = useState('');
  useEffect(() => {
    if (!sid) return;
    const abort = new AbortController();
    let timer: ReturnType<typeof setTimeout>;
    let blob = '';
    const poll = async () => {
      try {
        const response = await fetch(`/api/browser-test/${encodeURIComponent(sid)}/frame`, { signal: abort.signal, cache: 'no-store' });
        if (!response.ok) {
          const body = await response.json();
          throw new Error(body.error || 'Browser frame unavailable');
        }
        const image = await response.blob();
        if (abort.signal.aborted) return;
        const next = URL.createObjectURL(image);
        const previous = blob;
        blob = next;
        setFrame(next);
        setError('');
        if (previous) URL.revokeObjectURL(previous);
      } catch (err) {
        if (!abort.signal.aborted) setError(errorMessage(err));
      } finally {
        if (!abort.signal.aborted) timer = setTimeout(poll, 200);
      }
    };
    void poll();
    return () => { abort.abort(); clearTimeout(timer); if (blob) URL.revokeObjectURL(blob); };
  }, [sid]);
  return (
    <TileFrame id={id} kind="browsertest" selected={!!selected} label="Live browser testing" minW={320} minH={240}>
      <TileHead id={id} icon="monitor" title="Browser testing · Live" actions={
        <Button size="sm" variant="ghost" onClick={() => void apiRequest('DELETE', `/api/browser-test/${encodeURIComponent(sid)}`).catch((err) => setError(errorMessage(err)))}>Stop</Button>
      } />
      <div className="browser-test-display nodrag nopan nowheel">
        {frame && <img src={frame} alt="Live webpage under test, including agent cursor" />}
        {error && <p role="status">{error}</p>}
        {!frame && !error && <p role="status">Waiting for browser…</p>}
      </div>
    </TileFrame>
  );
}
export const BrowserTestTile = memo(BrowserTestTileImpl);

export function BrowserTestPicker({ workspaceId, onClose }: { workspaceId: string; onClose: () => void }) {
  const sessions = useSessionList((s) => s.workspaceId === workspaceId && s.status === 'running' && isAgentSession(s));
  const [chosen, setChosen] = useState('');
  const [url, setUrl] = useState('http://localhost:3000');
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const sid = sessions.some((s) => s.id === chosen) ? chosen : sessions[0]?.id ?? '';
  const open = async () => {
    setError('');
    try {
      const address = new URL(url.trim());
      if (!['http:', 'https:'].includes(address.protocol) || address.username || address.password) throw new Error('Enter an HTTP(S) URL without embedded credentials.');
      if (!sid) throw new Error('Start an agent console on this board first.');
      setBusy(true);
      await apiRequest('POST', `/api/browser-test/${encodeURIComponent(sid)}/open`, { url: address.href });
      onClose();
    } catch (err) {
      setError(errorMessage(err));
    } finally {
      setBusy(false);
    }
  };
  return (
    <Modal title="Browser testing" icon="monitor" onClose={onClose}
      help="Opens a live browser card beside the selected agent. Localhost means the hub machine."
      footer={<><Button onClick={onClose}>Cancel</Button><Button variant="primary" loading={busy} disabled={busy || !sid || !url.trim()} onClick={() => void open()}>Open browser</Button></>}>
      <form onSubmit={(e) => { e.preventDefault(); if (!busy) void open(); }}>
        <SelectField label="Agent console" value={sid} onChange={(e) => setChosen(e.target.value)} disabled={busy || !sessions.length}>
          {!sessions.length && <option value="">No running agent console</option>}
          {sessions.map((s) => <option key={s.id} value={s.id}>@{s.handle || s.name}</option>)}
        </SelectField>
        <TextField label="Webpage URL" type="url" required value={url} disabled={busy} onChange={(e) => setUrl(e.target.value)} />
        {!sessions.length && <p>Start an agent console on this board, then open Browser testing again.</p>}
        {error && <p role="alert">{error}</p>}
      </form>
    </Modal>
  );
}
