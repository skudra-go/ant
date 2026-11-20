---
id: 46
title: "Interceptors"
area: observability
status: active
tags: [Interceptor, WithLogger, LogValues, WithInterceptor, executor, otel]
issues: []
related: [68, 71]
changed_by: []
code: [intercept.go, internal/exec/observe.go, contrib/otel]
tests: [tests/features/developer_tooling/interceptor_test.go, tests/features/developer_tooling/logging_test.go, tests/concurrency/callbacks_test.go]
---

# 46. Interceptors

**In short:** Every statement passes the executor, where ant.Interceptor Before/After hooks serve logging, metrics and tracing; WithLogger is built on it and values are hidden unless LogValues is set.

## Decision

There is one point where every statement passes: the executor. `ant.Interceptor` wraps it with `Before` and `After`, which is enough for logging, metrics, tracing, and refusing or rewriting statements. `WithLogger` is built on it. OpenTelemetry and Prometheus packages are not included, because they would add dependencies. They are thin interceptors that can be added as sub-packages.

Values stay out of logs unless `ant.LogValues()` is used.
