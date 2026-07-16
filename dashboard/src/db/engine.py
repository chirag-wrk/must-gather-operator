from __future__ import annotations

from pathlib import Path
from typing import AsyncGenerator

from sqlalchemy.ext.asyncio import AsyncSession, async_sessionmaker, create_async_engine

from src.core.config import get_settings
from src.db.base import Base

_engine = None
_session_factory: async_sessionmaker[AsyncSession] | None = None


def _ensure_data_dir(url: str) -> None:
    """Create the parent directory for SQLite file URLs with proper permissions."""
    if url.startswith("sqlite"):
        db_path = url.split("///")[-1]
        parent = Path(db_path).parent
        parent.mkdir(parents=True, exist_ok=True)
        try:
            parent.chmod(0o777)
        except OSError:
            pass


def get_engine():
    global _engine
    if _engine is None:
        cfg = get_settings().database
        _ensure_data_dir(cfg.url)
        _engine = create_async_engine(cfg.url, echo=cfg.echo)
    return _engine


def get_session_factory() -> async_sessionmaker[AsyncSession]:
    global _session_factory
    if _session_factory is None:
        _session_factory = async_sessionmaker(
            get_engine(), class_=AsyncSession, expire_on_commit=False
        )
    return _session_factory


async def init_db() -> None:
    engine = get_engine()
    async with engine.begin() as conn:
        await conn.run_sync(Base.metadata.create_all)


async def get_db() -> AsyncGenerator[AsyncSession, None]:
    factory = get_session_factory()
    async with factory() as session:
        yield session
