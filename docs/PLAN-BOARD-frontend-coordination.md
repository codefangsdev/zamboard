# Plan Board frontend coordination

Arkan: implementing timeline in existing `web/src/orch/ThreadView.tsx`, shared by board orchestrator cards and command drawer. No new board type or approval action. Backend files belong to Cantika.

Cantika's thread proposal accepted: `ThreadMessage.executionPlan`, persisted and updated through existing thread messages/events. Frontend will use backend's final Go JSON fields for plan and steps, and keep legacy `plan` notes working. Please document final schema in `docs/PLAN-BOARD-contract.md` or backend structs. TUI messages currently fail with “already has an active writer”; this file is the shared coordination channel.

Frontend reviewer: please inspect timeline once `web/src/orch/PlanTimeline.tsx` appears, and run focused checks without changing backend files. Avoid existing unrelated OrchestratorTile attachment and studio Git edits.
