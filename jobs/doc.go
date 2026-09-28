// Package jobs provides typed background-job declarations and explicit,
// at-least-once delivery. Payloads are snapshots; handlers receive fresh values
// and constructor-injected services. A dispatch identity remains stable across
// redelivery, while each reservation gets a new lease owner.
//
// Prepare registrations and dispatchers without I/O. Backend lifecycle belongs
// to the caller. Memory backends are explicit test/local authorities, never a
// fallback for a failed durable backend. Application handlers must be idempotent
// and cooperate with context cancellation.
package jobs
