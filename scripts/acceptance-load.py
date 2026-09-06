#!/usr/bin/env python3
"""Open-loop event traffic plus four paced readers against an isolated panel."""
import argparse
import collections
import datetime
import http.client
import json
import pathlib
import queue
import threading
import time
import urllib.parse


class Metrics:
    def __init__(self):
        self.lock = threading.Lock()
        self.rows = {}
        self.failures = []

    def record(self, name, elapsed, status, error=""):
        with self.lock:
            row = self.rows.setdefault(name, {"count": 0, "errors": 0, "milliseconds": collections.Counter()})
            row["count"] += 1
            row["errors"] += int(status < 200 or status >= 300)
            row["milliseconds"][min(60000, int(elapsed * 1000))] += 1
            if (status < 200 or status >= 300) and len(self.failures) < 10:
                self.failures.append({"path": name, "status": status, "error": error[:300]})

    def summary(self):
        with self.lock:
            result = {}
            for name, row in self.rows.items():
                entry = {"count": row["count"], "errors": row["errors"]}
                for percentile in (50, 95, 99):
                    target = (row["count"] * percentile + 99) // 100
                    seen = 0
                    for ms, count in sorted(row["milliseconds"].items()):
                        seen += count
                        if seen >= target:
                            entry[f"p{percentile}_ms"] = ms
                            break
                result[name] = entry
            return result


class Client:
    def __init__(self, url, metrics):
        self.url, self.metrics, self.conn = url, metrics, None

    def call(self, method, path, token="", body=None, metric=None):
        started = time.monotonic()
        try:
            if self.conn is None:
                self.conn = http.client.HTTPConnection(self.url.hostname, self.url.port, timeout=15)
            headers = {"Content-Type": "application/json"}
            if token:
                headers["Authorization"] = "Bearer " + token
            data = json.dumps(body).encode() if body is not None else None
            self.conn.request(method, path, data, headers)
            response = self.conn.getresponse()
            payload = response.read()
            status = response.status
            # Framework timeouts may have an empty or plain-text body. Preserve
            # their HTTP status instead of reporting a JSON/transport failure.
            if status < 200 or status >= 300:
                self.metrics.record(metric or path, time.monotonic() - started, status, payload.decode(errors="replace"))
                return status, {}
            try:
                decoded = json.loads(payload)
            except ValueError as error:
                self.metrics.record(metric or path, time.monotonic() - started, 0, f"HTTP {status} invalid JSON: {error}")
                return 0, {}
            self.metrics.record(metric or path, time.monotonic() - started, status, payload.decode(errors="replace"))
            return status, decoded
        except Exception as error:
            if self.conn:
                self.conn.close()
            self.conn = None
            self.metrics.record(metric or path, time.monotonic() - started, 0, str(error))
            return 0, {}

    def close(self):
        if self.conn:
            self.conn.close()
            self.conn = None


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--dir", required=True)
    parser.add_argument("--url", required=True)
    parser.add_argument("--seconds", type=float, default=300)
    parser.add_argument("--rate", type=float, default=100)
    parser.add_argument("--readers", type=int, default=4)
    parser.add_argument("--think-seconds", type=float, default=0.25)
    args = parser.parse_args()
    url = urllib.parse.urlsplit(args.url)
    if url.scheme != "http" or url.hostname not in ("127.0.0.1", "localhost") or not url.port:
        parser.error("only an explicitly port-bound loopback test service is accepted")
    if args.seconds <= 0 or args.rate <= 0 or args.readers < 0:
        parser.error("invalid load settings")
    directory = pathlib.Path(args.dir)
    fixture = json.loads((directory / "fixture.json").read_text())
    if (directory / "expected.json").exists():
        parser.error("this fixture already has a load result; use a fresh run")
    metrics = Metrics()
    login = Client(url, metrics)
    status, result = login.call("POST", "/api/v1/auth/login", body={"email": fixture["admin_email"], "password": fixture["admin_password"]})
    if status != 200:
        raise RuntimeError(f"fixture login failed: {status} {result}")
    token = result["data"]["auth"]["token"]
    login.close()
    stop = threading.Event()

    diagnostic_stop = threading.Event()
    diagnostic_metrics = Metrics()
    drain_deadline = [float("inf")]
    expected_lock = threading.Lock()
    expected = {}
    last = {}
    queues = {node["id"]: queue.Queue(maxsize=200) for node in fixture["nodes"]}
    node_tokens = {node["id"]: node["token"] for node in fixture["nodes"]}
    sequences = collections.Counter()
    revisions = collections.Counter()
    paths = [
        "/api/v1/admin/users?paged=true&limit=50&offset=0",
        "/api/v1/admin/subscriptions?paged=true&limit=50&offset=0",
        "/api/v1/admin/subscriptions/1",
        "/api/v1/admin/traffic/records?paged=true&bucket=hour&limit=50&offset=0",
        "/api/v1/admin/traffic/trends",
        "/api/v1/admin/traffic/reconciliation?paged=true&limit=50&offset=0",
    ]

    def reader(index):
        client = Client(url, metrics)
        try:
            while not stop.is_set():
                client.call("GET", paths[index % len(paths)], token)
                index += 1
                stop.wait(args.think_seconds)
        finally:
            client.close()

    def sender(node_id):
        client = Client(url, metrics)
        try:
            while True:
                if stop.is_set() and time.monotonic() > drain_deadline[0]:
                    return
                try:
                    job = queues[node_id].get(timeout=0.5)
                except queue.Empty:
                    if stop.is_set():
                        return
                    continue
                try:
                    sub_id, event, cumulative = job
                    status, _ = client.call("POST", "/api/zero/events", node_tokens[node_id], event)
                    if 200 <= status < 300:
                        with expected_lock:
                            expected[sub_id] = max(expected.get(sub_id, 0), cumulative)
                finally:
                    queues[node_id].task_done()
        finally:
            client.close()

    def sample_runtime():
        client = Client(url, diagnostic_metrics)
        try:
            while True:
                status, response = client.call("GET", "/api/v1/admin/system-info?include_runtime=true", token)
                # Infrequent diagnostics can outlive the server's keepalive
                # idle timeout. Use a fresh connection for each observation.
                client.close()
                runtime = response.get("data", {}).get("runtime")
                with (directory / "runtime-samples.jsonl").open("a") as output:
                    output.write(json.dumps({"at": datetime.datetime.now(datetime.timezone.utc).isoformat(),
                                             "status": status, "runtime": runtime}) + "\n")
                if diagnostic_stop.wait(10):
                    break
        finally:
            client.close()

    workers = [threading.Thread(target=sender, args=(node,)) for node in queues]
    readers = [threading.Thread(target=reader, args=(i,)) for i in range(args.readers)]
    diagnostic_thread = threading.Thread(target=sample_runtime)
    diagnostic_thread.start()
    for thread in workers + readers:
        thread.start()
    started = time.monotonic()
    started_at = datetime.datetime.now(datetime.timezone.utc).isoformat()
    count = dropped = 0
    max_schedule_lag = 0
    samples = directory / "client-samples.jsonl"
    next_sample = started + 10
    try:
        while time.monotonic() - started < args.seconds:
            scheduled = started + count / args.rate
            time.sleep(max(0, scheduled - time.monotonic()))
            max_schedule_lag = max(max_schedule_lag, time.monotonic() - scheduled)
            sub = fixture["subscriptions"][count % len(fixture["subscriptions"])]
            sub_id, node = sub["id"], sub["node_id"]
            revisions[sub_id] += 1
            sequences[node] += 1
            cumulative = revisions[sub_id] * 3072
            event = {
                "schema_id": "zero.event.v1", "event_id": f'{fixture["run_id"]}-{count}',
                "event_type": "flow.updated", "source_id": f"node-{node}",
                "principal_key": sub["principal"], "core_instance_id": fixture["run_id"],
                "sequence": sequences[node], "config_revision": 1, "occurred_at_unix_ms": int(time.time() * 1000),
                "payload": {"flow_id": f"live-{sub_id}", "traffic": {"bytes_up": revisions[sub_id] * 1024, "bytes_down": revisions[sub_id] * 2048}},
            }
            jobs = [(sub_id, event, cumulative)]
            if count % 20 == 0:
                jobs.append((sub_id, event, cumulative))
            if count % 100 == 0 and sub_id in last:
                old, value = last[sub_id]
                jobs.append((sub_id, dict(old, event_id=old["event_id"] + "-late"), value))
            last[sub_id] = (event, cumulative)
            for job in jobs:
                try:
                    queues[node].put_nowait(job)
                except queue.Full:
                    dropped += 1
            count += 1
            if time.monotonic() >= next_sample:
                with samples.open("a") as stream:
                    stream.write(json.dumps({"elapsed_seconds": time.monotonic() - started, "metrics": metrics.summary(), "queue_depth": sum(q.qsize() for q in queues.values()), "generator_dropped": dropped}) + "\n")
                next_sample += 10
    finally:
        drain_deadline[0] = time.monotonic() + 30
        stop.set()
        for thread in workers + readers:
            thread.join()
        diagnostic_stop.set()
        diagnostic_thread.join()
        report = {"run_id": fixture["run_id"], "started_at": started_at, "elapsed_seconds": time.monotonic() - started,
                  "offered_unique_events": count, "generator_dropped": dropped, "max_schedule_lag_seconds": max_schedule_lag,
                  "rate": args.rate, "readers": args.readers, "think_seconds": args.think_seconds,
                  "undelivered_jobs": sum(q.qsize() for q in queues.values()),
                  "metrics": metrics.summary(), "failures": metrics.failures}
        report["diagnostic_metrics"] = diagnostic_metrics.summary()
        report["diagnostic_failures"] = diagnostic_metrics.failures
        (directory / "load-result.json").write_text(json.dumps(report, indent=2) + "\n")
        (directory / "expected.json").write_text(json.dumps({"run_id": fixture["run_id"], "expected_bytes": expected}, indent=2) + "\n")
    errors = sum(row["errors"] for row in report["metrics"].values())
    if dropped or errors or report["undelivered_jobs"]:
        raise SystemExit(f"load failed: generator_dropped={dropped}, HTTP_errors={errors}; see load-result.json")


if __name__ == "__main__":
    main()
