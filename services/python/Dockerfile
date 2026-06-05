FROM python:3.11-slim AS builder
RUN apt-get update && apt-get install -y --no-install-recommends \
    gcc libgeos-dev libproj-dev && \
    rm -rf /var/lib/apt/lists/*
COPY requirements.txt .
RUN pip install --no-cache-dir -r requirements.txt

FROM python:3.11-slim
RUN apt-get update && apt-get install -y --no-install-recommends \
    libgeos-c1v5 libproj25 && \
    rm -rf /var/lib/apt/lists/*
COPY --from=builder /usr/local/lib/python3.11/site-packages /usr/local/lib/python3.11/site-packages
COPY --from=builder /usr/local/bin /usr/local/bin
WORKDIR /app
COPY worker.py .
RUN mkdir -p analysis
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=2 \
  CMD python -c "import sqlite3; sqlite3.connect('queue_store/queue.db', timeout=2).execute('SELECT 1')" || exit 1
USER nobody
ENTRYPOINT ["python", "worker.py"]
