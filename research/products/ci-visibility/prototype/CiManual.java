// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0
// Real SDK manual API; this does not exercise JUnit/build-tool instrumentation.
import datadog.trace.api.civisibility.CIVisibility;
import datadog.trace.api.civisibility.DDTest;
import datadog.trace.api.civisibility.DDTestModule;
import datadog.trace.api.civisibility.DDTestSession;
import datadog.trace.api.civisibility.DDTestSuite;
import java.nio.file.Paths;

public class CiManual {
  public static void main(String[] args) {
    DDTestSession session = CIVisibility.startSession("research-ci-java", Paths.get("."), "manual", null);
    DDTestModule module = session.testModuleStart("fixture-module", null);
    DDTestSuite suite = module.testSuiteStart("fixture-suite", CiManual.class, null);
    DDTest test = suite.testStart("fixture-test", null, null);
    test.setTag("research.fixture", true);
    test.end(null);
    suite.end(null);
    module.end(null);
    session.end(null);
  }
}
