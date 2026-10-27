-- Provider finalization intent is financial recovery authority. Do not silently
-- discard pending reservations during rollback; archive and reconcile first.
-- The down migration intentionally retains this additive evidence table.
SELECT 1;
