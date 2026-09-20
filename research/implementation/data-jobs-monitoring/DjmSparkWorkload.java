// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0
import java.util.Arrays;
import org.apache.spark.sql.Row;
import org.apache.spark.sql.RowFactory;
import org.apache.spark.sql.SparkSession;
import org.apache.spark.sql.types.DataTypes;
import org.apache.spark.sql.types.StructType;

/** Actual Spark SQL workload. Native Datadog job/stage/SQL instrumentation comes from the agent. */
public class DjmSparkWorkload {
  public static void main(String[] args) {
    SparkSession spark = SparkSession.builder().master("local[2]").appName("ddot-spark-orders")
        .config("spark.ui.enabled", "false").config("spark.sql.warehouse.dir", "/tmp/ddot-spark-warehouse")
        .getOrCreate();
    try {
      StructType schema = new StructType().add("order_id", DataTypes.IntegerType)
          .add("amount", DataTypes.IntegerType);
      spark.createDataFrame(Arrays.asList(RowFactory.create(1, 10), RowFactory.create(2, 20),
          RowFactory.create(3, 30)), schema).createOrReplaceTempView("orders");
      Row summary = spark.sql("select count(*) as count, sum(amount) as total from orders").collectAsList().get(0);
      if (summary.getLong(0) != 3 || summary.getLong(1) != 60) throw new AssertionError(summary);
      System.out.println("{\"spark_version\":\"" + spark.version() + "\",\"count\":3,\"total\":60}");
    } finally {
      spark.stop();
    }
  }
}
