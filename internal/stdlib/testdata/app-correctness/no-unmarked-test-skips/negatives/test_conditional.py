import sys
import pytest

@pytest.mark.skipif(sys.platform == "win32", reason="no fork on windows")
def test_fork():
    assert fork()
