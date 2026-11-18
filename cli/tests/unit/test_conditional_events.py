import pytest

from inferyard.errors import EvidenceError
from inferyard.evidence.conditional_events import queue_delay, startup, token_intervals


def test_content_chunks_and_missing_lifecycle_never_substitute_true_events():
    assert token_intervals({"source_semantics": "content_chunks"}, "c")["value"] is None
    assert queue_delay({}, "c")["value"] is None
    assert startup({}, "c")["first_valid_answer_seconds"] is None
