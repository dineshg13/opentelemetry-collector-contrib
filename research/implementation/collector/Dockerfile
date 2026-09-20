# Copyright The OpenTelemetry Authors
# SPDX-License-Identifier: Apache-2.0
FROM alpine:3.22.2
COPY ddot-products-collector /ddot-products-collector
USER 65532:65532
EXPOSE 4317 4318 8126 13133 8888
ENTRYPOINT ["/ddot-products-collector"]
CMD ["--config=/conf/collector.yaml"]
