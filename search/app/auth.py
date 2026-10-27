import hashlib
from dataclasses import dataclass, field

ROLES = ("viewer", "operator", "admin")


def hash_key(key: str) -> str:
    # API keys are high-entropy random strings, so an unsalted SHA-256 is enough (no brute-force-able passwords)
    return hashlib.sha256(key.encode()).hexdigest()


@dataclass(frozen=True)
class Principal:
    user: str
    role: str
    team: str
    collections: list[str] = field(default_factory=list)


def has_role(p: Principal, minimum: str) -> bool:
    return ROLES.index(p.role) >= ROLES.index(minimum)
