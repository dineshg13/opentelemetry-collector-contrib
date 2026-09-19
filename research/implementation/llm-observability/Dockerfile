# Copyright The OpenTelemetry Authors
# SPDX-License-Identifier: Apache-2.0
FROM python:3.12.13-slim
WORKDIR /app
COPY requirements.txt .
RUN pip install --no-cache-dir -r requirements.txt
COPY app.py .
USER 65532:65532
EXPOSE 8080
ENTRYPOINT ["python", "-u", "app.py"]
