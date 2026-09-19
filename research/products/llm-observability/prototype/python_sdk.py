# Copyright The OpenTelemetry Authors
# SPDX-License-Identifier: Apache-2.0
"""Real Python SDK exercise. The parent runner supplies isolated import/Agent settings."""
import json
import ddtrace
from ddtrace.llmobs import LLMObs

assert ddtrace.__version__ == '4.13.0rc1', ddtrace.__version__
LLMObs.enable(agent_service='research', agentless_enabled=False, integrations_enabled=False)
with LLMObs.llm(name='research.runtime', model_name='test-model', model_provider='custom') as span:
    LLMObs.annotate(input_data=[{'role': 'user', 'content': 'hello'}],
                    output_data=[{'role': 'assistant', 'content': 'world'}],
                    metrics={'input_tokens': 1, 'output_tokens': 1})
    span_context = LLMObs.export_span(span)
LLMObs.submit_evaluation(label='quality', metric_type='score', value=1, span=span_context)
LLMObs.flush()
LLMObs.disable()
ddtrace.tracer.shutdown()
print(json.dumps({'sdk_version': ddtrace.__version__, 'sdk_module': ddtrace.__file__}))
