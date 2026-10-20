# Preserve each selected observation's source material

Buffered envelopes already retain receiver time and sender-declared SourceID.
Aggregation selects Latest and StatsEvent independently, but the internal mapping
to network.EventPosition dropped both fields. The typed position now retains each
selected event's own original SourceID and ReceivedAt. It copies bytes/times
without normalization or replay-time substitution. Missing legacy receipt time
remains zero/unknown, and a flow-only projection does not invent a stats sample.

SourceID is sender-declared metadata, not authenticated installation identity,
proof of a host/runtime, configuration confirmation or authorization renewal.
The two existing event comparators remain byte-identical; receiver time/source
do not change sequencing or billing. There is no new public HTTP/JSON contract,
spool format, persisted cursor, schema migration or permission/financial change.
The audit found no public serialization path or unkeyed EventPosition literal.

The regression captures a later flow and an independently selected stats sample,
including reversed receipt times, different runtimes, exact whitespace in source,
legacy zero fields, no-stats and repeat mapping. Seven comparator cases each run
against three metadata variations. A controlled old mapping removes only the two
new field assignments while retaining the new type/tests for compilation: three
leaf source-preservation assertions fail as expected. Fixed network/handler and
race-count3 pass; see the [hash-bound receipt](validation/2026-10-20-event-position-source-material.json).
This is source preservation, not asynchronous projection or a DB integration test.

Existing persisted spool records predate ReceivedAt and legitimately lack it.
File mtime, replay time, today's credentials and heartbeat/ACK cannot reconstruct
that receipt or installed-host identity. Accounting still shares its transaction
with node stats and Latest/Stats still share a DB cursor. Durable auxiliary intent,
independent stats ordering, fault isolation, lifecycle/tombstones and bounded
retention remain P4 work. No legacy facts are fabricated. Rollback to an older
binary needs no schema changes but resumes discarding the two fields at mapping.
