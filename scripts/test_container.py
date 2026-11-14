#!/usr/bin/env python3

"""Run the runtime image with Quadlet-equivalent restrictions and disposable data."""

import base64
import hashlib
import io
import json
import socket
import sqlite3
import subprocess
import tempfile
import time
import urllib.error
import urllib.request
import zipfile
from pathlib import Path


def main():
    with tempfile.TemporaryDirectory(prefix="hoshilet-container-") as tmp:
        root = Path(tmp)
        data, config_dir = root / "data", root / "config"
        config_dir.mkdir()
        config = json.loads(Path("deploy/config.example.json").read_text())
        config.update(
            data_dir=str(data),
            min_free_bytes=0,
            listen="0.0.0.0:8080",
            server_message="Container notice\n实例提示",
        )
        native = root / "native.json"
        native.write_text(json.dumps(config))
        subprocess.run(
            [
                "bin/hoshilet-server",
                "-config",
                str(native),
                "-backup",
                str(root / "initial.tar.gz"),
            ],
            check=True,
        )
        now = int(time.time())
        with sqlite3.connect(data / "database/hoshilet.db") as db:
            db.execute(
                "INSERT INTO users(id,gitlab_id,username,personal_ns,status) VALUES('user',1,'tester','ns','approved')"
            )
            db.execute(
                "INSERT INTO namespaces(id,name,created_at) VALUES('ns','tester',?)",
                [now],
            )
            db.execute("INSERT INTO members VALUES('ns','user','admin')")
            db.execute(
                "INSERT INTO credentials(id,hash,user_id,kind,expires_at,created_at) VALUES('test',?,'user','key',?,?)",
                [hashlib.sha256(b"test-key").hexdigest(), now + 3600, now],
            )
        config["data_dir"] = "/var/lib/hoshilet"
        (config_dir / "config.json").write_text(json.dumps(config))
        (config_dir / "config.json").chmod(0o600)
        with socket.socket() as s:
            s.bind(("127.0.0.1", 0))
            port = s.getsockname()[1]
        name = "hoshilet-runtime-test-" + str(port)
        base = "http://127.0.0.1:" + str(port)
        command = [
            "podman",
            "run",
            "--rm",
            "-d",
            "--name",
            name,
            "--userns=keep-id:uid=10001,gid=10001",
            "--read-only",
            "--cap-drop=ALL",
            "--security-opt=no-new-privileges",
            "--tmpfs=/tmp:rw,size=64m",
            "-p",
            f"127.0.0.1:{port}:8080",
            "-v",
            f"{data}:/var/lib/hoshilet:z",
            "-v",
            f"{config_dir}:/etc/hoshilet:ro,z",
            "localhost/hoshilet:0.1.0",
            "-config",
            "/etc/hoshilet/config.json",
        ]
        subprocess.run(command, check=True, stdout=subprocess.DEVNULL)

        def request(path, method="GET", body=None, headers=None):
            h = {"Authorization": "Bearer test-key", **(headers or {})}
            with urllib.request.urlopen(
                urllib.request.Request(
                    base + path, data=body, headers=h, method=method
                ),
                timeout=30,
            ) as r:
                assert (
                    base64.b64decode(r.headers["X-Hoshilet-Server-Message"]).decode()
                    == config["server_message"]
                )
                return json.load(r)

        def ready():
            for _ in range(100):
                try:
                    if request("/healthz")["status"] == "ok":
                        return
                except (OSError, urllib.error.URLError):
                    time.sleep(0.1)
            raise RuntimeError("container did not become healthy")

        def deploy(content, idem):
            b = io.BytesIO()
            with zipfile.ZipFile(b, "w", zipfile.ZIP_DEFLATED) as z:
                z.writestr("index.html", content)
            return request(
                "/api/v1/deploy?project=docs",
                "POST",
                b.getvalue(),
                {"Content-Type": "application/zip", "Idempotency-Key": idem},
            )

        try:
            ready()
            first = deploy("one", "container-first")
            second = deploy("two", "container-second")
            pid = first["project_id"]
            q = request("/api/v1/projects/" + pid)
            request(
                "/api/v1/projects/" + pid + "/activate",
                "POST",
                json.dumps(
                    {"deployment_id": first["deployment_id"], "revision": q["revision"]}
                ).encode(),
                {"Content-Type": "application/json"},
            )
            assert (data / "public/tester/docs/index.html").read_text() == "one"
            subprocess.run(
                ["podman", "restart", name], check=True, stdout=subprocess.DEVNULL
            )
            ready()
            q = request("/api/v1/projects/" + pid)
            assert q["current_id"] == first["deployment_id"]
            assert (data / "public/tester/docs/index.html").read_text() == "one"
            print(
                "Runtime image: non-root, read-only rootfs, upload worker, publication, rollback, restart: PASS"
            )
        except Exception:
            subprocess.run(["podman", "logs", name], check=False)
            raise
        finally:
            subprocess.run(
                ["podman", "rm", "-f", name], check=False, stdout=subprocess.DEVNULL
            )


if __name__ == "__main__":
    main()
