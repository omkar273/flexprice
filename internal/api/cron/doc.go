// Package cron exposes HTTP routes that manually trigger jobs otherwise run on a schedule.
//
// Deprecated: for recurring automation, use Temporal server schedules and the /v1/temporal
// endpoints (e.g. POST /v1/temporal/setup, GET /v1/temporal/schedules). The /v1/cron/... routes
// are legacy or on-call entrypoints; they mirror the same work as Temporal-driven workflows
// where applicable.
package cron
