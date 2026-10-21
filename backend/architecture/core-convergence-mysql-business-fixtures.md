# P7: explicit MySQL business fixtures

Repeated seven-step schema preparation is unnecessary for a business test whose
assertions concern already-initialized transactions. Seventy explicitly reviewed
top-level tests now use independent random schemas restored from an immutable
in-process snapshot of the actual preparation. This changes test setup only.
The original publish/order factories remain; initialization, permissions,
recorded migrations, lost tables/columns, interrupted DDL and database-copy
tests continue to execute the real preparation and original negative assertions.
No production schema, migration or business write path changes.

The snapshot executes the original seven preparation functions in an isolated
schema, before services start, captures complete SHOW CREATE and all initial
seeds, then drops its own schema. It retains no live pool/database. Targets use
complete CREATE statements with foreign keys enabled, in actual FK order, and
transactionally restore bounded initial seeds. Unknown objects, cross-schema
references, cross-table cycles, unexpected business rows and populated
self-reference seeds refuse. The actual empty protocol endpoint self-reference
keeps its full FK in CREATE; orphan rejection and ON DELETE SET NULL are tested.
No production-copy shortcut, CREATE LIKE, FK/session-setting rewrite, live
schema sharing, retry/fallback or received business fact is introduced.

Equality includes complete schema/default/index/CHECK/FK/options/AUTO_INCREMENT,
seeds and physical column type/null/default/extra/charset/collation/generation
metadata. MySQL may re-render a redundant CHARACTER SET for a COLLATE column:
only that direct metadata-proven prefix is made explicit for comparison.
Installation uses original SHOW CREATE verbatim. A real collation change must
remain unequal. Independent real preparations may differ only in identified
generated seed clocks; clone-versus-template compares every value exactly.

[Exact source/commands and retained failures](validation/2026-10-21-mysql-business-fixtures.json):
six actual MySQL contracts pass; focused race3 has18 top-level executions,
zero skips. All40 selected business cases and4 contracts pass in actual MySQL,
98.688s command wall. The existing business assertions, injection triggers,
locks, native plugin/RPC setup and post-fixture real reconciliation are retained.
Explicit scope is37 order and3 handlers factories, not a global wrapper switch.

Paired n3 quiet-window setup seconds are3.648886407/3.329003025/3.329533969
original versus1.178726249/1.185131956/1.580753189 snapshot install. One bootstrap
costs3.700789375s. Every measured fresh target passes complete equivalence outside
the timed setup. Candidate has105 CREATE/2 SELECT/154 INSERT/1 COMMIT versus
original120 CREATE/70 ALTER/15 CREATE INDEX/1771 SELECT/97 INSERT/50 COMMIT.
These are actual session commands, including repeated preparation CREATEs,
not unique-table counts or a whole-suite saving. Extra INSERTs, bootstrap and
contract-test cost must be included in the eventual aggregate result.

Initial FK-cycle refusal, lexical DDL equality failures, soft-delete test mistake
and unused patch-artifact errors remain with exact frozen sources/log hashes.
The original900s failures remain. Full integrated backend/MySQL and a new exact
CI still need execution; the per-setup result does not declare P7 complete.

Rollback is a test-source revert, with no production migration or user-data
conversion. Each test owns/cleans only its random schema. Original0001 bootstrap
retains its published historical FK0/1 statements; clone install always requires
FK1 and never changes it. Future initialization changes require renewing this
explicit fixture contract and actual schema/seed equivalence evidence.


## Explicit expansion to70 business tests

The second batch substitutes only30 additional reviewed top-level factory
expressions in9 test files: receipt4, automatic-renewal10, plan-group7,
plan-quota5, network2 and runtime2. It adds35 independent business schemas;
combined70 business tests own75 schemas. Additional template contracts are
outside that75. No shared factory is switched and no original helper/assertion,
lock probe, timeout, trigger, plugin/native operation or migration tail changes.
Original40 and the snapshot implementation remain byte-identical.

Actual MySQL70 business+4 contracts pass in154.905s command wall. Seven added
contention paths pass race/count3,21 top-level executions,71.105s command wall;
zero skips. This is selected-suite correctness evidence, not a measured full
900s result. Current integrated full backend/original MySQL/new CI remain pending.
[Exact33-source scope and execution](validation/2026-10-21-mysql-business-fixtures-expanded.json).
The parent907 local full Go/vet/build/CGO0SQLite and all three original MySQL
commands passed; its CI still timed out at900.021s. Both results are retained,
not replaced by prior ae2 green CI or this selected-suite pass.
