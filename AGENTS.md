# Zboard agent instructions

- Read `PROJECT_MEMORY.md` before starting non-trivial repository work.
- Preserve the resource boundary documented there and in `docs/data-model.md`.
- For every implementation goal, run proportionate local verification, append
  the goal outcome and remaining gaps to `PROJECT_MEMORY.md`, then synchronize
  the verified working tree with `scripts/sync-intranet.ps1`.
- After synchronization, verify the deployed version, `/readyz`, container
  health and goal-specific behavior. Record the version, database backup,
  previous-source path and evidence in `PROJECT_MEMORY.md`.
- A failed or unavailable synchronization is a remaining gap, not a completed
  deployment. Report it explicitly.
- Read-only analysis, reviews and explanations do not trigger deployment.
- Never write credentials, subscription URLs, private keys or environment-file
  contents into repository memory or logs.
- Synchronizing the intranet environment does not authorize Git staging,
  commits, pushes or releases; those still require explicit user direction.
