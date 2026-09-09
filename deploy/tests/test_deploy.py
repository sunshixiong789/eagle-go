"""Run release scripts against a Docker command double; never contact a daemon."""
import json
import os
from pathlib import Path
import shutil
import subprocess
import sys
import tempfile
import time
import unittest


ROOT = Path(__file__).resolve().parents[2]
IMAGE_A = "registry.example.com/team/eagle@sha256:" + "a" * 64
IMAGE_B = "registry.example.com/team/eagle@sha256:" + "b" * 64
DOCKER = r'''
import json, os, pathlib, signal, sys, time
args = sys.argv[1:]
if args in (["compose", "version"], ["buildx", "version"]):
    sys.exit(0)
if args[:2] == ["buildx", "build"]:
    pathlib.Path(args[args.index("--metadata-file") + 1]).write_text(json.dumps({
        "containerimage.digest": os.environ.get("TEST_BUILD_DIGEST", "sha256:" + "a" * 64)
    }))
    sys.exit(int(os.environ.get("TEST_BUILD_EXIT", "0")))
assert args[0] == "compose", args
def read_env(path):
    return {k: v[1:-1] for k, v in (line.split("=", 1) for line in pathlib.Path(path).read_text().splitlines())}
config = read_env(args[args.index("--env-file") + 1])
# Model Compose shell > --env-file precedence, so a leaking EAGLE_IMAGE breaks tests.
config.update({k: v for k, v in os.environ.items() if k.startswith("EAGLE_")})
stage = next(x for x in args if x in ("config", "pull", "run", "up", "rm", "ps"))
event = {"stage": stage, "image": config["EAGLE_IMAGE"],
         "port": config.get("EAGLE_HTTP_PORT"), "args": args,
         "runtime": read_env(config["EAGLE_ENV_FILE"]),
         "compose": pathlib.Path(args[args.index("-f") + 1]).read_text()}
with open(os.environ["TEST_DOCKER_LOG"], "a") as log:
    log.write(json.dumps(event) + "\n")
if stage == os.environ.get("TEST_SLEEP_STAGE"):
    time.sleep(1)
if stage == os.environ.get("TEST_SIGNAL_STAGE") and config["EAGLE_IMAGE"].endswith("b" * 64):
    os.kill(os.getppid(), signal.SIGTERM)
if stage == os.environ.get("TEST_FAIL_STAGE") and (
    not os.environ.get("TEST_FAIL_IMAGE") or config["EAGLE_IMAGE"] == os.environ["TEST_FAIL_IMAGE"]
):
    sys.exit(23)
'''


class DeploymentTests(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        self.root = Path(self.tmp.name)
        self.repo = self.root / "repo"
        shutil.copytree(ROOT / "deploy", self.repo / "deploy", ignore=shutil.ignore_patterns("__pycache__"))
        self.bin = self.root / "bin"
        self.bin.mkdir()
        self.executable("docker", DOCKER)
        # Use a real advisory lock, including on macOS where util-linux isn't installed.
        self.executable("flock", "import fcntl, sys\ntry:\n fcntl.flock(int(sys.argv[-1]), fcntl.LOCK_EX | fcntl.LOCK_NB)\nexcept BlockingIOError:\n sys.exit(1)\n")
        self.keys = self.root / "keys"
        self.keys.mkdir()
        (self.keys / "test-key.pem").write_text("test fixture, not a private key")
        self.log = self.root / "docker.jsonl"
        self.env = {k: v for k, v in os.environ.items() if not k.startswith(("EAGLE_", "CI_", "FLOW_"))}
        self.env.update({
            "PATH": str(self.bin) + os.pathsep + os.environ["PATH"],
            "TEST_DOCKER_LOG": str(self.log), "DEPLOY_ENV": "testing",
            "EAGLE_DEPLOY_ROOT": str(self.root / "host"), "EAGLE_IMAGE": IMAGE_A,
            "EAGLE_DATABASE_DSN": "postgres://user:pa$word%23@db/eagle",
            "EAGLE_AUTH_ISSUER": "https://auth.example.com", "EAGLE_AUTH_AUDIENCE": "test-api",
            "EAGLE_AUTH_SIGNING_KEY_HOST_DIRECTORY": str(self.keys),
            "EAGLE_AUTH_ACTIVE_SIGNING_KEY_ID": "test-key",
        })
        self.host = self.root / "host" / "testing"

    def executable(self, name, code):
        path = self.bin / name
        path.write_text("#!" + sys.executable + "\n" + code)
        path.chmod(0o755)

    def run_script(self, script="deploy.sh", action="deploy", success=True, **changes):
        result = subprocess.run(["sh", str(self.repo / "deploy/scripts" / script), action],
                                env=self.env | changes, capture_output=True, text=True)
        self.assertEqual(result.returncode == 0, success, result.stdout + result.stderr)
        self.assertNotIn(self.env["EAGLE_DATABASE_DSN"], result.stdout + result.stderr)
        return result

    def events(self):
        return [json.loads(line) for line in self.log.read_text().splitlines()] if self.log.exists() else []

    def snapshot(self, pointer="current"):
        return self.host / "releases" / (self.host / pointer).read_text().strip()

    def test_success_keeps_private_snapshot_and_migrates_before_start(self):
        self.run_script()
        self.assertEqual([x["stage"] for x in self.events()], ["config", "pull", "run", "up", "ps"])
        snapshot = self.snapshot()
        self.assertEqual((snapshot / "runtime.env").stat().st_mode & 0o777, 0o600)
        self.assertNotIn("EAGLE_DATABASE_DSN", (snapshot / "compose.env").read_text())
        self.assertEqual(self.events()[-1]["runtime"]["EAGLE_DATABASE_DSN"], self.env["EAGLE_DATABASE_DSN"])
        self.assertFalse((self.host / "previous").exists())

    def test_swagger_is_only_enabled_for_development(self):
        for environment in ("development", "testing", "production"):
            with self.subTest(environment=environment):
                self.run_script(DEPLOY_ENV=environment, EAGLE_SERVER_SWAGGER_ENABLED="true",
                                EAGLE_SERVER_SWAGGER_PATH="/dev/docs")
                runtime = self.events()[-1]["runtime"]
                self.assertEqual(runtime["EAGLE_SERVER_SWAGGER_ENABLED"],
                                 "true" if environment == "development" else "false")
                if environment == "development":
                    self.assertEqual(runtime["EAGLE_SERVER_SWAGGER_PATH"], "/dev/docs")

    def test_pull_and_migration_failure_leave_both_success_pointers_intact(self):
        self.run_script()
        self.run_script(EAGLE_IMAGE=IMAGE_B)
        before = [(self.host / x).read_text() for x in ("current", "previous")]
        for stage in ("pull", "run"):
            self.log.unlink()
            self.run_script(success=False, TEST_FAIL_STAGE=stage)
            self.assertNotIn("up", [x["stage"] for x in self.events()])
            self.assertEqual([(self.host / x).read_text() for x in ("current", "previous")], before)

    def test_failed_health_restores_image_config_and_compose_despite_new_env(self):
        self.run_script()
        old_snapshot = self.snapshot()
        old_compose = (old_snapshot / "compose.yml").read_text()
        with (self.repo / "deploy/compose.app.yml").open("a") as target:
            target.write("\n# new release manifest\n")
        self.run_script(success=False, EAGLE_IMAGE=IMAGE_B, EAGLE_HTTP_PORT="8800",
                        EAGLE_DATABASE_DSN="postgres://new@db/new", TEST_FAIL_STAGE="up", TEST_FAIL_IMAGE=IMAGE_B)
        starts = [x for x in self.events() if x["stage"] == "up"]
        self.assertEqual([x["image"] for x in starts], [IMAGE_A, IMAGE_B, IMAGE_A])
        self.assertEqual(starts[-1]["port"], "8000")
        self.assertEqual(starts[-1]["runtime"]["EAGLE_DATABASE_DSN"], self.env["EAGLE_DATABASE_DSN"])
        self.assertEqual(starts[-1]["compose"], old_compose)
        self.assertEqual(self.snapshot(), old_snapshot)

    def test_manual_rollback_uses_previous_snapshot_without_migrations(self):
        self.run_script()
        self.run_script(EAGLE_IMAGE=IMAGE_B)
        self.log.unlink()
        self.run_script(script="cd.sh", action="rollback", EAGLE_DATABASE_DSN="", EAGLE_IMAGE=IMAGE_B)
        self.assertEqual([x["stage"] for x in self.events()], ["config", "up", "ps"])
        self.assertEqual(self.events()[-1]["image"], IMAGE_A)
        self.assertIn(IMAGE_B, (self.snapshot("previous") / "compose.env").read_text())

    def test_first_release_failure_removes_only_failed_app(self):
        self.run_script(success=False, TEST_FAIL_STAGE="up")
        self.assertEqual(self.events()[-1]["stage"], "rm")
        self.assertFalse((self.host / "current").exists())

    def test_failed_rollback_reports_failure(self):
        self.run_script()
        result = self.run_script(success=False, EAGLE_IMAGE=IMAGE_B, TEST_FAIL_STAGE="up")
        self.assertIn("rollback failed", result.stderr)
        self.assertIn(IMAGE_A, (self.snapshot() / "compose.env").read_text())

    def test_signal_during_start_restores_previous(self):
        self.run_script()
        result = self.run_script(success=False, EAGLE_IMAGE=IMAGE_B, TEST_SIGNAL_STAGE="up")
        self.assertEqual(result.returncode, 143)
        self.assertEqual([x["image"] for x in self.events() if x["stage"] == "up"][-2:], [IMAGE_B, IMAGE_A])

    def test_invalid_images_and_secret_values_fail_before_pull(self):
        for value in ("repo:latest", "repo:abcdef", IMAGE_A + "\nextra"):
            self.run_script(success=False, EAGLE_IMAGE=value)
        for value in ("quote'password", "line\npassword", "line\rpassword"):
            self.run_script(success=False, EAGLE_DATABASE_DSN=value)
        self.assertEqual(self.events(), [])

    def test_concurrent_deployment_cannot_modify_active_snapshot(self):
        process = subprocess.Popen(["sh", str(self.repo / "deploy/scripts/deploy.sh")],
                                   env=self.env | {"TEST_SLEEP_STAGE": "pull"},
                                   stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
        try:
            deadline = time.monotonic() + 5
            while not any(x["stage"] == "pull" for x in self.events()):
                self.assertLess(time.monotonic(), deadline)
                time.sleep(0.02)
            self.run_script(success=False, EAGLE_IMAGE=IMAGE_B)
            self.assertEqual(process.wait(timeout=10), 0)
            self.assertEqual({x["image"] for x in self.events()}, {IMAGE_A})
        finally:
            if process.poll() is None:
                process.terminate()
                process.wait(timeout=10)

    def test_legacy_layout_is_adopted_for_rollback(self):
        self.host.mkdir(parents=True)
        values = self.env | {"EAGLE_HTTP_PORT": "8000"}
        (self.host / "runtime.env").write_text("".join(f"{k}='{v}'\n" for k, v in values.items() if k.startswith("EAGLE_")))
        (self.host / "compose.yml").write_text("# legacy manifest\n")
        self.run_script(success=False, EAGLE_IMAGE=IMAGE_B, TEST_FAIL_STAGE="up", TEST_FAIL_IMAGE=IMAGE_B)
        self.assertEqual(self.events()[-1]["image"], IMAGE_A)
        self.assertEqual(self.events()[-1]["compose"], "# legacy manifest\n")

    def test_cd_rejects_conflicting_pipeline_image(self):
        (self.repo / "release").mkdir()
        (self.repo / "release/image.txt").write_text(IMAGE_A + "\n")
        self.run_script(script="cd.sh", success=False, EAGLE_IMAGE=IMAGE_B)
        self.assertEqual(self.events(), [])

    def test_build_release_packages_only_deploy_files_and_digest(self):
        for args in (["init", "-q"], ["add", "."], ["-c", "user.name=CI", "-c", "user.email=ci@example.com", "commit", "-qm", "fixture"]):
            subprocess.run(["git", *args], cwd=self.repo, check=True, capture_output=True)
        (self.repo / ".git/info/exclude").write_text("dist/\n")
        self.run_script(script="build-release.sh", EAGLE_IMAGE_REPOSITORY="registry.example.com/team/eagle")
        import tarfile
        with tarfile.open(self.repo / "dist/eagle-release.tgz") as artifact:
            files = {x.name for x in artifact.getmembers() if x.isfile()}
            self.assertEqual(files, {"deploy/compose.app.yml", "deploy/scripts/cd.sh", "deploy/scripts/deploy.sh",
                                     "deploy/scripts/registry-login.sh", "release/image.txt", "release/commit.txt"})
            self.assertEqual(artifact.extractfile("release/image.txt").read().decode().strip(), IMAGE_A)
        self.run_script(script="build-release.sh", success=False,
                        EAGLE_IMAGE_REPOSITORY="registry.example.com/team/eagle", TEST_BUILD_DIGEST="invalid")
        self.assertFalse((self.repo / "dist/eagle-release.tgz").exists())

    def test_coverage_gate_does_not_mask_test_failure(self):
        self.executable("go", "import sys\nif sys.argv[1] == 'test': sys.exit(23)\nprint('total: (statements) 99.0%')\n")
        result = subprocess.run(["make", "-f", str(ROOT / "Makefile"), "test-coverage"],
                                cwd=self.root, env=self.env, capture_output=True, text=True)
        self.assertNotEqual(result.returncode, 0)

    def test_ci_rejects_a_missing_or_self_comparison_base(self):
        self.run_script(script="ci.sh", action="check", success=False, EAGLE_CI_BASE_REF="")
        for args in (["init", "-q"], ["add", "."], ["-c", "user.name=CI", "-c", "user.email=ci@example.com", "commit", "-qm", "fixture"]):
            subprocess.run(["git", *args], cwd=self.repo, check=True, capture_output=True)
        result = self.run_script(script="ci.sh", action="check", success=False, EAGLE_CI_BASE_REF="HEAD")
        self.assertIn("base equals HEAD", result.stderr)
        self.assertEqual(self.events(), [])

    def test_each_environment_uses_the_same_image_with_isolated_variables_and_state(self):
        for environment in ("development", "testing", "production"):
            self.run_script(DEPLOY_ENV=environment, EAGLE_AUTH_AUDIENCE="eagle-api-" + environment)
            host = self.root / "host" / environment
            snapshot = host / "releases" / (host / "current").read_text().strip()
            self.assertEqual(self.events()[-1]["image"], IMAGE_A)
            self.assertEqual(self.events()[-1]["runtime"]["EAGLE_AUTH_AUDIENCE"], "eagle-api-" + environment)
            self.assertIn("eagle-" + environment, self.events()[-1]["args"])
            self.assertNotIn("EAGLE_OBSERVABILITY_LOG_LEVEL", (snapshot / "runtime.env").read_text())

    def test_missing_required_variables_and_unknown_environment_fail_before_pull(self):
        self.run_script(success=False, DEPLOY_ENV="../testing")
        for name in ("EAGLE_DATABASE_DSN", "EAGLE_AUTH_ISSUER", "EAGLE_AUTH_AUDIENCE", "EAGLE_AUTH_ACTIVE_SIGNING_KEY_ID"):
            self.run_script(success=False, **{name: ""})
        self.assertEqual(self.events(), [])

    def test_explicit_overrides_are_preserved_in_runtime_snapshot(self):
        self.run_script(EAGLE_OBSERVABILITY_LOG_LEVEL="warn", EAGLE_OBSERVABILITY_OTLP_ENDPOINT="",
                        EAGLE_DATABASE_MAX_CONNS="40", EAGLE_SERVER_HTTP_TIMEOUT="8s")
        runtime = self.events()[-1]["runtime"]
        self.assertEqual(runtime["EAGLE_OBSERVABILITY_LOG_LEVEL"], "warn")
        self.assertEqual(runtime["EAGLE_OBSERVABILITY_OTLP_ENDPOINT"], "")
        self.assertEqual(runtime["EAGLE_DATABASE_MAX_CONNS"], "40")
        self.assertEqual(runtime["EAGLE_SERVER_HTTP_TIMEOUT"], "8s")


if __name__ == "__main__":
    unittest.main()
