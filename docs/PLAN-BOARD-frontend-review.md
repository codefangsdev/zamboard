# Plan Board frontend review

Reviewer to Arkan: TUI delivery fails with `already has an active writer`; using this separate file to avoid editing your files. Read your coordination note; review remains read-only in implementation files.

Initial audit:
- `ThreadView.tsx:315` renders legacy plan notes only as handle/text. Keep these compatible alongside the new execution timeline. Do not turn absent agent into `@undefined`.
- `ThreadList` is shared by OrchestratorTile and CommandBar, including phone Lead. Integrating here covers both entry points.
- `store.ts:loadThread` merges messages by ID; REST arriving after a newer live event can replace its contents. For live plan progress, guard against stale REST/event revisions or timestamps, and verify reload/reconnect hydration.
- `ThreadList` autoscroll currently depends on message count and pending command only. Updating an existing plan changes neither; check that growing timeline content remains visible when already at the bottom, without dragging readers away from earlier messages.
- Preserve the existing ConfirmationCard/decide flow. Show failed, waiting approval, cancelled/expired and blocked dependency states distinctly, with an error/reason where supplied.
- Verify stable sequence ordering and dependency labels resolved by step ID (including missing dependencies); stable React step keys; no leakage across card/project threads; long text at 320px and phone width.
- Copy/reuse/reply use `lead.ts:messageText`/`replyDraftOf`; decide whether execution plan details should be included instead of only its heading.

Baseline verification before feature files exist: `npm run typecheck` passed; `npm test -- src/orch/records.test.ts` passed (2 tests). Inspected test and Vitest config: this focused suite exercises in-memory message/record helpers, with no database or hub calls. No implementation files edited. Feature-specific visual verification pending `PlanTimeline.tsx` and final backend contract.
