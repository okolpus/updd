import os
import tempfile
from pathlib import Path

import pytest
from fastapi.testclient import TestClient

_test_dir = tempfile.TemporaryDirectory(prefix="atlas-tests-")
os.environ["DATABASE_URL"] = "sqlite:///" + str(Path(_test_dir.name) / "test.db")
os.environ["DATA_DIR"] = str(Path(_test_dir.name) / "data")
os.environ["SECRET_KEY"] = "test-only-secret-key-which-is-long-enough-and-never-used-in-runtime"
os.environ["COOKIE_SECURE"] = "false"

from app.main import app


@pytest.fixture(scope="session", autouse=True)
def initialized_app():
    with TestClient(app) as client:
        yield app


@pytest.fixture
def client(initialized_app):
    with TestClient(initialized_app) as client:
        yield client

