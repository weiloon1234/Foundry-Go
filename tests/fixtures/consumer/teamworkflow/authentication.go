package teamworkflow

import "foundry.test/consumer/internal/authfixture"

// Actor reuses the shared loopback identity without coupling application fixtures.
type Actor = authfixture.Actor
