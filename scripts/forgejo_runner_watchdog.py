#!/usr/bin/env python3

import os
import pathlib
import sqlite3
import subprocess
import sys
import time


def env_int(name: str, default: int) -> int:
    value = os.getenv(name, "").strip()
    if value == "":
        return default
    return int(value)


def main() -> int:
    db_path = os.getenv("FORGEJO_DB_PATH", "/opt/forgejo/forgejo/data/gitea.db")
    runner_dir = os.getenv("FORGEJO_RUNNER_DIR", "/opt/forgejo-runner-masazo")
    runner_service = os.getenv("FORGEJO_RUNNER_SERVICE", "runner")
    queue_age = env_int("FORGEJO_RUNNER_QUEUE_AGE_SECONDS", 90)
    cooldown = env_int("FORGEJO_RUNNER_RESTART_COOLDOWN_SECONDS", 180)
    repo_id = os.getenv("FORGEJO_RUNNER_REPO_ID", "").strip()
    state_dir = pathlib.Path(
        os.getenv("FORGEJO_RUNNER_WATCHDOG_STATE_DIR", "/var/lib/forgejo-runner-watchdog")
    )
    state_dir.mkdir(parents=True, exist_ok=True)
    stamp_path = state_dir / "last_restart"

    now = int(time.time())
    threshold = now - queue_age

    conn = sqlite3.connect(db_path)
    try:
        cur = conn.cursor()
        running_query = "select count(*) from action_run_job where status = 6"
        queued_query = (
            "select count(*) from action_run_job "
            "where status = 5 and task_id = 0 and created <= ?"
        )
        params = [threshold]
        if repo_id != "":
            running_query += " and repo_id = ?"
            queued_query += " and repo_id = ?"
            params.append(int(repo_id))
            running_count = cur.execute(running_query, (int(repo_id),)).fetchone()[0]
        else:
            running_count = cur.execute(running_query).fetchone()[0]
        queued_count = cur.execute(queued_query, params).fetchone()[0]
    finally:
        conn.close()

    if running_count > 0:
        print("runner is busy; nothing to do")
        return 0
    if queued_count == 0:
        print("no stale queued jobs")
        return 0

    if stamp_path.exists():
        last_restart = int(stamp_path.read_text(encoding="utf-8").strip() or "0")
        if now-last_restart < cooldown:
            print("cooldown active; skipping restart")
            return 0

    subprocess.run(
        ["docker", "compose", "restart", runner_service],
        cwd=runner_dir,
        check=True,
        stdout=subprocess.DEVNULL,
        stderr=subprocess.STDOUT,
    )
    stamp_path.write_text(str(now), encoding="utf-8")
    print(f"restarted {runner_service} due to {queued_count} stale queued job(s)")
    return 0


if __name__ == "__main__":
    try:
        raise SystemExit(main())
    except Exception as exc:
        print(f"watchdog failed: {exc}", file=sys.stderr)
        raise
