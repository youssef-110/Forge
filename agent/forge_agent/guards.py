"""Input guard: cheap regex heuristics for prompt injection and secret exfiltration.

shortcut: regexes miss paraphrased attacks; upgrade to a classifier (e.g. Llama Guard /
NeMo Guardrails) if eval guardrail_injection recall drops.
"""

import re

_PATTERNS: list[tuple[str, re.Pattern[str]]] = [
    ("instruction override", re.compile(
        r"\b(ignore|disregard|forget|override)\b.{0,40}\b(previous|prior|above|earlier|all|your|system)\b.{0,20}\b(instructions?|prompts?|rules?|guidelines?|directives?)",
        re.I | re.S)),
    ("system prompt extraction", re.compile(
        r"\b(print|reveal|show|repeat|output|dump|leak|tell me)\b.{0,40}\b(system|hidden|initial)\s+(prompt|instructions?|message)",
        re.I | re.S)),
    ("secret exfiltration", re.compile(
        r"\b(print|reveal|show|give|send|output|dump|leak|echo|tell me|what is|what's)\b.{0,40}\b(api[\s_-]?keys?|x-api-key|secrets?|passwords?|credentials?|tokens?|env(ironment)? var(iable)?s?|FORGE_API_KEY)\b",
        re.I | re.S)),
    ("role escalation", re.compile(
        r"\b(you are now|act as|pretend (to be|you are)|switch to)\b.{0,30}\b(admin|root|developer mode|DAN|unrestricted)\b",
        re.I | re.S)),
    ("fake system tag", re.compile(r"<\s*/?\s*(system|sys|admin)\s*>|\[\s*(system|INST)\s*\]", re.I)),
]

REFUSAL = (
    "I can't help with that: the request looks like an attempt to override my instructions "
    "or extract secrets ({reason}). I can search the runbooks or inspect and manage jobs "
    "within your role's permissions."
)


def check_input(text: str) -> str | None:
    """Return the matched rule name if the text should be blocked, else None."""
    for name, pattern in _PATTERNS:
        if pattern.search(text):
            return name
    return None
