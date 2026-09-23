# Copyright The OpenTelemetry Authors
# SPDX-License-Identifier: Apache-2.0
FROM python:3.12.13-slim
RUN apt-get update && apt-get install -y --no-install-recommends git && rm -rf /var/lib/apt/lists/*
WORKDIR /app
COPY requirements.txt .
RUN pip install --no-cache-dir -r requirements.txt
COPY run.py test_workload.py ./
RUN git init -q && git config user.name 'DDOT CI fixture' \
    && git config user.email 'ddot-ci@example.invalid' \
    && git add run.py test_workload.py \
    && git -c commit.gpgsign=false commit -qm 'Add representative CI fixture' \
    && git remote add origin https://example.invalid/ddot-ci-fixture.git \
    && chown -R 65532:65532 /app
USER 65532:65532
ENTRYPOINT ["python", "-u", "run.py"]
