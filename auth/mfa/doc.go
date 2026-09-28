// Package mfa owns typed TOTP/recovery factors and model-first factor management.
// Its manager rechecks passwords, locks models before factors, applies a separate
// MFA throttle, and persists replay/consumption with model changes and credential
// revocation. PostgreSQL stores encrypted TOTP secrets through explicit migrations.
// Management never issues authenticated credentials. TOTP uses six-digit HMAC-SHA1
// with a 30-second period; inputs and results preserve model and factor types.
package mfa
