// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

// Research workload: real Java SDK/JFR upload, no external services.
public class ProfileWorkload {
  public static void main(String[] args) {
    long deadline = System.nanoTime() + 5_000_000_000L;
    double checksum = 0;
    while (System.nanoTime() < deadline) {
      for (int i = 1; i < 10000; i++) {
        checksum += Math.sqrt(i);
      }
    }
    System.out.println("profile-workload-complete " + (checksum > 0));
  }
}
