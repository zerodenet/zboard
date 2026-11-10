# Zero event spool startup and recovery

## Critical path

The HTTP listener starts after route composition. Required service setup remains
synchronous: opening HTTP early must not bypass ownership, schema or durable
spool recovery. Each composition stage logs its name, result and elapsed time.
`zero_event_spool_recovery` also logs segment count, checkpoint and sequence floor.
These timings distinguish migration, core setup, spool recovery and plugin setup
from container/cutover timing.

Sealed event history is immutable. Startup enumerates filenames and verifies only
mutable active tails. It does not decompress or JSON-decode sealed history. A torn
last append can be truncated as before; a complete corrupt active record remains
at its original path and prevents startup on every retry. A non-tail active file
is sealed before a new writable tail is allocated.

Sealed payload validation belongs to consumption, before projection. ReadBatch
checks codec, block checksum, record framing/count and record CRCs. Unreadable
events cannot be acknowledged or silently quarantined away. Pending counters
read compressed headers with seek, without decompressing event bodies. A metadata
failure is explicitly unknown and schedules the consumer to report its error;
it is never represented as a healthy empty queue.

Commit may use header bounds only for the exact verified in-flight next cursor
returned by ReadBatch. Arbitrary cursors still require full validation. Block
count validation prevents a damaged header from hiding trailing events when a
limited batch ends at the declared boundary. This does not weaken accounting
idempotency, retention or support for historical reports.

## Monotonic physical sequence

The writer retains an in-process sequence high-water mark across sealing and
reclamation. On restart, its initial floor is at least the durable checkpoint and
the largest retained segment. Empty directories therefore cannot restart at 1
behind an existing cursor. Sequence exhaustion fails instead of wrapping.

Older writers could reuse numbers after the last sealed file was reclaimed. If
every retained segment is below the durable checkpoint, startup, under exclusive
spool ownership, rebases the selected authoritative files above that checkpoint.
Only filenames change. Original event IDs, envelopes, source timestamps, proof
material and bodies stay byte-for-byte unchanged. The durable checkpoint is not
reset or advanced. Existing durable accounting idempotency handles replay of
events that were booked before the old cursor stopped progressing.

Recovery writes one bounded intent journal (maximum 16 MiB), fsyncs it, validates
the entire source/destination mapping, renames, and fsyncs the directory. A restart
can resume a partially renamed plan. Changed checkpoints/files, duplicate targets,
missing files, unsafe paths and ambiguous non-tail active files fail closed with
the original evidence retained. The completion receipt replaces one metadata
file; it is not an additional event archive. No new per-event rows or body copies
are introduced. Obsolete duplicate encodings follow the existing authoritative
representation and reclamation rules.

## Verification

`internal/zeroevent` covers lazy sealed recovery, active corruption across repeated
starts, monotonic allocation after reclamation, partial-journal restart, conflicts,
corrupt compressed metadata/body and exact verified-prefix commit. The race suite
checks reader/writer/maintenance access. The handler accounting replay test books
a prefix first, rebases the original events and verifies the final usage is added
exactly once. Benchmarks compare full historical scanning, active-tail recovery
and header-only status on the same retained-history fixtures.

Local benchmarks establish scaling and allocation behavior, not a production
startup guarantee. A later explicitly authorized rollout must measure the logged
phases and external readiness, then verify checkpoint progress and accounting
through the retained backlog. A local test does not constitute production replay.
