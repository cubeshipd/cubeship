import contextlib
import io
import os
import subprocess
import sys
import unittest
from unittest.mock import MagicMock, patch
from urllib.error import URLError

import start


class StartupTests(unittest.TestCase):
    @patch.dict(os.environ, {"CUBESHIP_HONCHO_ROLE": "api"}, clear=True)
    def test_failed_migration_prevents_api_start(self):
        with patch.object(start, "wait_until_ready") as wait, \
             patch.object(start.subprocess, "run", side_effect=subprocess.CalledProcessError(1, "migration")), \
             patch.object(start.os, "execv") as execute:
            with self.assertRaises(subprocess.CalledProcessError):
                start.main()
            wait.assert_called_once_with(start.database_ready, "PostgreSQL")
            execute.assert_not_called()

    @patch.dict(os.environ, {"CUBESHIP_HONCHO_ROLE": "api"}, clear=True)
    def test_api_migrates_before_handing_over_process(self):
        steps = []
        with patch.object(start, "wait_until_ready", side_effect=lambda *args: steps.append("ready")), \
             patch.object(start.subprocess, "run", side_effect=lambda *args, **kwargs: steps.append("migrated")), \
             patch.object(start.os, "execv", side_effect=lambda *args: steps.append("started")) as execute:
            start.main()
        self.assertEqual(steps, ["ready", "migrated", "started"])
        self.assertIn("src/main.py", execute.call_args.args[1])

    @patch.dict(os.environ, {"CUBESHIP_HONCHO_ROLE": "deriver"}, clear=True)
    def test_worker_waits_for_api_and_does_not_migrate(self):
        with patch.object(start, "wait_until_ready") as wait, \
             patch.object(start.subprocess, "run") as migrate, \
             patch.object(start.os, "execv") as execute:
            start.main()
        wait.assert_called_once_with(start.api_ready, "the Honcho API")
        migrate.assert_not_called()
        execute.assert_called_once_with(sys.executable, [sys.executable, "-m", "src.deriver"])

    @patch.dict(os.environ, {"CUBESHIP_HONCHO_ROLE": "deriver"}, clear=True)
    def test_unavailable_api_prevents_worker_start(self):
        with patch.object(start, "wait_until_ready", side_effect=RuntimeError("timeout")), \
             patch.object(start.os, "execv") as execute:
            with self.assertRaises(RuntimeError):
                start.main()
            execute.assert_not_called()

    def test_readiness_retries_then_times_out(self):
        check = MagicMock(return_value=False)
        with patch.object(start.time, "monotonic", side_effect=[0, 0, 2]), \
             patch.object(start.time, "sleep") as sleep, \
             contextlib.redirect_stdout(io.StringIO()):
            with self.assertRaisesRegex(RuntimeError, "Timed out waiting for PostgreSQL"):
                start.wait_until_ready(check, "PostgreSQL", timeout=2)
        self.assertEqual(check.call_count, 2)
        sleep.assert_called_once_with(2)

    def test_readiness_can_recover(self):
        check = MagicMock(side_effect=[False, True])
        with patch.object(start.time, "sleep"), contextlib.redirect_stdout(io.StringIO()):
            start.wait_until_ready(check, "PostgreSQL")
        self.assertEqual(check.call_count, 2)

    @patch.dict(os.environ, {"DB_CONNECTION_URI": "postgresql+psycopg://honcho:private@example:5432/honcho"})
    def test_database_probe_hides_connection_errors(self):
        driver = MagicMock()
        driver.OperationalError = ConnectionError
        driver.connect.side_effect = ConnectionError("private credentials")
        output = io.StringIO()
        with patch.dict(sys.modules, {"psycopg": driver}), contextlib.redirect_stdout(output):
            self.assertFalse(start.database_ready())
        self.assertNotIn("private", output.getvalue())
        driver.connect.assert_called_once_with(
            "postgresql://honcho:private@example:5432/honcho", connect_timeout=3
        )

    @patch.dict(os.environ, {"CUBESHIP_HONCHO_API_URL": "http://api:8000/"})
    def test_api_probe_handles_startup_connection_failure(self):
        with patch.object(start.urllib.request, "urlopen", side_effect=URLError("not ready")) as request:
            self.assertFalse(start.api_ready())
        request.assert_called_once_with("http://api:8000/health", timeout=3)

    @patch.dict(os.environ, {"CUBESHIP_HONCHO_ROLE": "invalid"}, clear=True)
    def test_unknown_role_fails_without_starting_anything(self):
        with patch.object(start.os, "execv") as execute:
            with self.assertRaises(ValueError):
                start.main()
            execute.assert_not_called()


if __name__ == "__main__":
    unittest.main()
