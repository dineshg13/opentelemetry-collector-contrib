SELECT
  calls,
  datname,
  shared_blks_dirtied, 
  shared_blks_hit,
  shared_blks_read,
  shared_blks_written,
  temp_blks_read,
  temp_blks_written,
  query,
  queryid::TEXT,
  pg_stat_statements.dbid::TEXT AS dbid,
  pg_stat_statements.userid::TEXT AS userid,
  -- PG13 has no toplevel column; JSON lookup preserves compatibility.
  COALESCE(to_jsonb(pg_stat_statements)->>'toplevel', 'true') AS toplevel,
  COALESCE(rolname, '') AS rolname,
  rows::TEXT,
  total_exec_time,
  total_plan_time
FROM
  pg_stat_statements as pg_stat_statements
  LEFT JOIN pg_roles ON pg_stat_statements.userid = pg_roles.oid
  INNER JOIN pg_database ON pg_stat_statements.dbid = pg_database.oid
WHERE
  datname IS NOT NULL
  AND query != '<insufficient privilege>'
  AND query NOT LIKE '/* otel-collector-ignore */%'
ORDER BY calls DESC
LIMIT 31;