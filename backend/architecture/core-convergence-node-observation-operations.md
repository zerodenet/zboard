# P4/P5: bounded observation queue metadata

The node-management page has a global observation queue drawer. It opens
independently of the selected node and lists held, pending, claimed or applied
metadata, including historical records for deleted or replaced assets. A failed
read retains the last successful page and its status; stale requests cannot
replace a later selection or reopen. Reads happen only on open or explicit
refresh/page/filter actions, with no polling or automatic write replay.

GET `/api/v1/admin/node-observation-intents` uses the application capability and
current active-administrator database predicate as well as the HTTP permission
check. Status is single-valued, the default is held, page size defaults to50
and is bounded1–100. A canonical version1 cursor binds status and UTC millisecond
`(available_at,id)`. Unknown/duplicate parameters, malformed/noncanonical cursors
and mixed-status positions fail explicitly. The query uses LIMIT+1, without a
total COUNT, OFFSET, node/reason scan or loading/decompressing retained parts.

The DTO exposes only opaque source reference, historical node number,
disposition/reason, exact decimal-string diagnostic counts, queue times and
claimed-only lease time. It excludes credentials, claim token, asset tag,
principal/CoreID/source/flow identities and payload. A current-asset relationship
is metadata only; material_verified is always false. Queue/disposition timestamps
are not event sampling, freshness, online or configuration-confirmation evidence.
The UI uses fixed reason labels and safe unknown-code fallbacks. Unknown data
must not become a numeric zero or a new authorization decision.

MySQL retains a binary asset comparison and adds ordinary equality to make both
existing composite-PK columns usable. The real same-data query-plan diagnostic
uses5000 historical asset states,1 current state,60 headers and two limit20
pages. Cast-only state lookups visited5001 rows per loop; the final equality plus
binary fence used node_id+asset_tag with one row per lookup. Each EXPLAIN ANALYZE
is one execution, not HTTP p95 or a proof of large-queue growth bounds. Physical
case/padding mismatch fixtures verify no other asset's deletion marker is used.

There is no new migration, production data conversion, repair/requeue/delete
endpoint or financial write. Reverting this API/UI batch preserves all0060
headers, immutable parts, dispositions, state and receipts. Existing0060
cutover/old-writer restrictions still apply. No external kernel/plugin capability
is required; this is panel queue metadata, not an external integration claim.

**Retention remains open.** Applied parts that are no longer referenced by
Latest/Stats/Floor are still required by duplicate-source replay validation
inside the authority transaction. Deleting them would fail settlement/spool ACK.
Referenced parts, held/pending/claimed material, state/tombstones, headers,
receipts and authoritative metering recovery data remain intact.0056 diagnostic
retention cannot be reused as a queue TTL. A future archive/crop contract must
cover replay, exact source comparison, copy, crash consistency and old-binary
compatibility before any source deletion. This batch does not reduce database
growth or complete P4 recovery operations/P6 scaling.

Validation and retained candidate failures are recorded in
[the exact source receipt](validation/2026-10-21-node-observation-operations.json).
Only SQLite/MySQL isolated tests, Vue component/request behavior tests and
contract/build checks are claimed; no production migration or browser/kernel
end-to-end acceptance was performed for this new drawer. Full integrated
backend and exact-head CI status are recorded separately in the phase plan.


1012实际Chromium154验收已执行：真实fixture登录、held默认、applied两页各50且不重叠、
计数原字符串/时间、关闭重开首屏、独立节点抽屉及读请求审计通过。二进制/PID/端口、
535个前端输入、实际served index/assets均有绑定，原完整容量export前后不变。
第一次最终隔离审计因原版本检查GET被拦截而失败，原失败保留；第二脚本仍阻断所有
跨站请求，仅准确识别固定公共版本GET的预期阻断，其他异常仍失败，无mock/种数。
[两次原始结果及脚本差异](validation/2026-10-21-node-observation-operations-browser.json)。
实际小计数不证明2^53边界，该边界仍由专项双库合同证明；不宣称生产或长期容量通过。
