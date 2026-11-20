---
id: 97
title: "Spans name the database the dialect talks to"
area: observability
status: active
tags: [otel, db.system, WithDialect, WithSystem, tracing, Interceptor]
issues: [61]
related: []
changed_by: []
code: [contrib/otel/otel.go]
tests: [contrib/otel/otel_test.go]
---

# 97. Spans name the database the dialect talks to

**In short:** contrib/otel takes WithDialect or WithSystem to set db.system, mapping ant dialect names to OpenTelemetry values and defaulting to postgresql.

## Decision

`contrib/otel` set `db.system` to `postgresql` on every span, with no way to change it, so traces from the other four databases were mislabelled (#61). An interceptor never learns the context's dialect, so `New` takes options: `WithDialect` maps ant's dialect names to OpenTelemetry's values (`postgresql`, `mysql`, `mariadb`, `sqlite`, `mssql`), and passes another dialect's name through, and `WithSystem` sets the value as given. Having `ant.Open` tell interceptors the dialect was rejected: it would add a hidden interface to the core for one attribute. The default stays `postgresql`, so existing setups keep their spans.
