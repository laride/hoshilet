#!/usr/bin/env python3

"""Real Caddy routing/atomic-publication contract. CADDY points to a test binary."""

import http.client
import os
import socket
import subprocess
import tempfile
import time
import urllib.error
import urllib.request
from pathlib import Path


def port():
    with socket.socket() as sock:
        sock.bind(("127.0.0.1", 0))
        return sock.getsockname()[1]


class NoRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, *args, **kwargs):
        return None


def main():
    with tempfile.TemporaryDirectory(prefix="hoshilet-static-") as tmp:
        root = Path(tmp)
        root.chmod(0o755)
        management = root / "admin"
        management.mkdir()
        (management / "index.html").write_text("standalone admin")
        (management / "app.js").write_text("console.log(1)")
        public = root / "public"
        (public / "alice").mkdir(parents=True)
        for version in ["v1", "v2"]:
            release = root / "releases" / version
            release.mkdir(parents=True)
            (release / "index.html").write_text(version)
            (release / "asset.js").write_text("console.log(1)")
        (public / "alice" / "docs").symlink_to("../../releases/v1")
        (public / "bob").symlink_to("../releases/v1")
        content, admin = port(), port()
        source = Path("deploy/Caddyfile").read_text()
        source = (
            source.replace("hoshilet-admin.example.com", f"http://127.0.0.1:{admin}")
            .replace("hoshilet.example.com", f"http://127.0.0.1:{content}")
            .replace("/var/lib/hoshilet/public", str(public))
            .replace("/srv/hoshilet/admin", str(management))
        )
        config = root / "Caddyfile"
        config.write_text("{\n admin off\n auto_https off\n}\n" + source)
        logfile = open(root / "caddy.log", "w+")
        env = dict(
            os.environ,
            XDG_DATA_HOME=str(root / "xdg-data"),
            XDG_CONFIG_HOME=str(root / "xdg-config"),
        )
        server = os.environ.get("STATIC_SERVER", "caddy")
        if server == "nginx":
            env = dict(os.environ)
            nginx = (
                Path("deploy/nginx.conf")
                .read_text()
                .replace("/var/lib/hoshilet/public", "/srv/public")
                .replace("/srv/hoshilet/admin", "/srv/admin")
            )
            nginx = nginx.replace("listen 443 ssl;", "listen 8081;", 1).replace(
                "listen 443 ssl;", "listen 8080;", 1
            )
            nginx = "\n".join(
                line for line in nginx.split("\n") if "ssl_certificate" not in line
            )
            config.write_text(nginx)
            command = [
                "podman",
                "run",
                "--rm",
                "--name",
                "hoshilet-nginx-test-" + str(content),
                "-p",
                f"127.0.0.1:{content}:8080",
                "-p",
                f"127.0.0.1:{admin}:8081",
                "-v",
                f"{root}:/srv:ro,z",
                "-v",
                f"{config}:/etc/nginx/conf.d/default.conf:ro,z",
                os.environ.get("NGINX_IMAGE", "docker.io/library/nginx:1.28-alpine"),
            ]
        else:
            command = [
                os.environ.get("CADDY", "caddy"),
                "run",
                "--config",
                str(config),
                "--adapter",
                "caddyfile",
            ]
        proc = subprocess.Popen(
            command, env=env, stdout=logfile, stderr=subprocess.STDOUT
        )
        opener = urllib.request.build_opener(NoRedirect)

        def get(path, server_port=content):
            try:
                res = opener.open(f"http://127.0.0.1:{server_port}" + path, timeout=3)
                return res.status, res.read(), res.headers
            except urllib.error.HTTPError as e:
                return e.code, e.read(), e.headers

        try:
            for _ in range(1200):
                if proc.poll() is not None:
                    logfile.seek(0)
                    raise RuntimeError(logfile.read())
                try:
                    status, body, headers = get("/~alice/docs/")
                    break
                except (
                    urllib.error.URLError,
                    ConnectionError,
                    http.client.HTTPException,
                ):
                    time.sleep(0.05)
            else:
                raise AssertionError("Caddy did not become ready")
            assert (status, body) == (200, b"v1"), (status, body, dict(headers))
            assert get("/", admin)[1] == b"standalone admin"
            assert get("/app.js", admin)[0] == 200
            assert get("/missing.js", admin)[0] == 404
            assert get("/", admin)[2].get("Content-Security-Policy")
            status, _, headers = get("/~alice/docs")
            assert (
                status == 308 and headers["Location"].split("?")[0] == "/~alice/docs/"
            ), dict(headers)
            assert get("/~bob/")[1] == b"v1"
            assert get("/alice/docs/")[0] == 404
            assert get("/~alice/docs/missing")[0] == 404
            assert get("/~alice/")[0] == 404
            assert get("/~alice/docs/../../../database/hoshilet.db")[0] in (400, 404)
            for version in ["v2", "v1"]:
                candidate = root / "staging-link"
                candidate.symlink_to("../../releases/" + version)
                candidate.replace(public / "alice" / "docs")
                assert get("/~alice/docs/")[1] == version.encode()
            # Creating another project requires no Caddy reload.
            (public / "alice" / "new").symlink_to("../../releases/v2")
            assert get("/~alice/new/")[1] == b"v2"
            (public / "alice" / "docs").unlink()
            assert get("/~alice/docs/")[0] == 404
            print(
                server
                + " routing, root/project sites, URL isolation, publication, rollback, create/delete: PASS"
            )
        except Exception:
            logfile.flush()
            logfile.seek(0)
            print(logfile.read())
            raise
        finally:
            if server == "nginx":
                subprocess.run(
                    ["podman", "rm", "-f", "hoshilet-nginx-test-" + str(content)],
                    env=env,
                    stdout=subprocess.DEVNULL,
                    stderr=subprocess.DEVNULL,
                    timeout=15,
                )
            else:
                proc.terminate()
            try:
                proc.wait(timeout=10)
            except subprocess.TimeoutExpired:
                proc.kill()
                proc.wait()
            logfile.close()


if __name__ == "__main__":
    main()
