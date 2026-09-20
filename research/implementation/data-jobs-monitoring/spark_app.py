# Copyright The OpenTelemetry Authors
# SPDX-License-Identifier: Apache-2.0
"""An actual local Spark SQL job; its JVM is instrumented by dd-java-agent."""
import json
from pyspark.sql import SparkSession

spark = SparkSession.builder.master("local[2]").appName("ddot-spark-orders").config("spark.ui.enabled", "false").config("spark.sql.warehouse.dir", "/tmp/ddot-spark-warehouse").getOrCreate()
try:
    frame = spark.createDataFrame([(1, 10), (2, 20), (3, 30)], ["order_id", "amount"])
    frame.createOrReplaceTempView("orders")
    summary = spark.sql("select count(*) as count, sum(amount) as total from orders").collect()[0]
    assert summary["count"] == 3 and summary["total"] == 60
    print(json.dumps({"spark_version": spark.version, "count": summary["count"], "total": summary["total"]}), flush=True)
finally:
    spark.stop()
