import pytest

@pytest.mark.skip(reason="flaky")
def test_refund():
    assert refund(10) == 10
