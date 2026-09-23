#!/bin/sh
# Copyright The OpenTelemetry Authors
# SPDX-License-Identifier: Apache-2.0
set -eu
psql --username "$POSTGRES_USER" --dbname "$POSTGRES_DB" --set=ON_ERROR_STOP=1 --set=monitor_password="$POSTGRES_PASSWORD" <<'SQL'
CREATE ROLE dbm_monitor LOGIN PASSWORD :'monitor_password';
GRANT pg_monitor TO dbm_monitor;
CREATE ROLE dbm_app LOGIN PASSWORD :'monitor_password';
GRANT CONNECT ON DATABASE dbm TO dbm_app;
CREATE EXTENSION pg_stat_statements;
SQL
psql --username "$POSTGRES_USER" --dbname postgres --set=ON_ERROR_STOP=1 -c 'CREATE EXTENSION pg_stat_statements;'
