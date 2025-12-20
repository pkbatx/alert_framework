FROM python:3.11-slim

WORKDIR /app

ENV PYTHONDONTWRITEBYTECODE=1 \
    PYTHONUNBUFFERED=1 \
    PYTHONPATH=/app/skills/caad_skillkit:/app

RUN apt-get update \
    && apt-get install -y --no-install-recommends curl \
    && rm -rf /var/lib/apt/lists/*

COPY skills/caad_skillkit /app/skills/caad_skillkit
COPY core /app/core
COPY contracts /app/contracts
COPY worker /app/worker

RUN pip install --no-cache-dir \
    click \
    requests \
    pydantic \
    jsonschema \
    openai \
    fastapi \
    uvicorn

CMD ["uvicorn", "caad_skillkit.api.app:create_app", "--factory", "--host", "0.0.0.0", "--port", "8090"]
