package storemigration_test

import (
	"context"
	"database/sql/driver"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/weiloon1234/Foundry-Go/attribution"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
	"github.com/weiloon1234/Foundry-Go/internal/attachmentstore"
	"github.com/weiloon1234/Foundry-Go/internal/auditstore"
	"github.com/weiloon1234/Foundry-Go/internal/extensionstore"
	"github.com/weiloon1234/Foundry-Go/internal/notificationstore"
	"github.com/weiloon1234/Foundry-Go/internal/webhookstore"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/temporal"
	"github.com/weiloon1234/Foundry-Go/testkit"
	"github.com/weiloon1234/Foundry-Go/value"
)

// These transcripts are captured from the generated stores before their declarations move.
func TestStage7Slice3bStoreParity(t *testing.T) {
	at, _ := temporal.NewDateTime(instant.Add(-3600 * 1000000000))
	cases := []struct {
		name    string
		columns []string
		values  []driver.Value
		run     func(context.Context, *database.Tx) (string, error)
	}{
		{name: "webhookstore_endpoint_read", columns: []string{"id", "url", "events", "active", "created_at", "updated_at"}, values: []driver.Value{uuid, "stored", `[]`, false, instant, instant}, run: func(ctx context.Context, tx *database.Tx) (string, error) {
			v, e := orm.First(ctx, tx, webhookstore.Endpoints.Query().Where(orm.KeyEq(webhookstore.Endpoints.ID, model.IDFromBytes[webhookstore.Endpoint]([16]byte{1, 137, 15, 71, 26, 0, 112, 0, 128, 0, 0, 0, 0, 0, 0, 1}))))
			return fingerprint(v, e)
		}},
		{name: "webhookstore_endpoint_insert", columns: []string{"id", "url", "events", "active", "created_at", "updated_at"}, values: []driver.Value{uuid, "stored", `[]`, false, instant, instant}, run: func(ctx context.Context, tx *database.Tx) (string, error) {
			v, e := orm.Insert(ctx, tx, webhookstore.Endpoints.Table, orm.KeySet(webhookstore.Endpoints.ID, model.IDFromBytes[webhookstore.Endpoint]([16]byte{1, 137, 15, 71, 26, 0, 112, 0, 128, 0, 0, 0, 0, 0, 0, 1})), orm.TextSet(webhookstore.Endpoints.URL, "inserted"), orm.JSONSet(webhookstore.Endpoints.Events, migrationJSON[[]string](t, `[]`)), orm.ColumnSet(webhookstore.Endpoints.Active, true), orm.TimeSet(webhookstore.Endpoints.CreatedAt, at), orm.TimeSet(webhookstore.Endpoints.UpdatedAt, at))
			return fingerprint(v, e)
		}},
		{name: "webhookstore_endpoint_write", columns: []string{"id", "url", "events", "active", "created_at", "updated_at"}, values: []driver.Value{uuid, "stored", `[]`, false, instant, instant}, run: func(ctx context.Context, tx *database.Tx) (string, error) {
			v, e := orm.Update(ctx, tx, webhookstore.Endpoints.Query(), webhookstore.Endpoints.Key(), model.IDFromBytes[webhookstore.Endpoint]([16]byte{1, 137, 15, 71, 26, 0, 112, 0, 128, 0, 0, 0, 0, 0, 0, 1}), orm.TextSet(webhookstore.Endpoints.URL, "updated"))
			return fingerprint(v, e)
		}},
		{name: "webhookstore_endpoint_identity", columns: []string{"id", "url", "events", "active", "created_at", "updated_at"}, values: []driver.Value{uuid, "stored", `[]`, false, instant, instant}, run: func(ctx context.Context, tx *database.Tx) (string, error) {
			v, e := orm.Require(ctx, tx, webhookstore.Endpoints.Query().Where(orm.KeyEq(webhookstore.Endpoints.Key(), model.IDFromBytes[webhookstore.Endpoint]([16]byte{1, 137, 15, 71, 26, 0, 112, 0, 128, 0, 0, 0, 0, 0, 0, 1}))))
			if e != nil {
				return "", e
			}
			id, e := orm.IdentityOf(webhookstore.Endpoints.Table, webhookstore.Endpoints.Key()).ReferenceOf(v).Identity()
			return fingerprint(id, e)
		}},
		{name: "webhookstore_secret_read", columns: []string{"id", "endpoint_id", "ciphertext", "expires_at", "created_at"}, values: []driver.Value{uuid, uuid, "stored", nil, instant}, run: func(ctx context.Context, tx *database.Tx) (string, error) {
			v, e := orm.First(ctx, tx, webhookstore.Secrets.Query().Where(orm.KeyEq(webhookstore.Secrets.ID, model.IDFromBytes[webhookstore.Secret]([16]byte{1, 137, 15, 71, 26, 0, 112, 0, 128, 0, 0, 0, 0, 0, 0, 1}))))
			return fingerprint(v, e)
		}},
		{name: "webhookstore_secret_insert", columns: []string{"id", "endpoint_id", "ciphertext", "expires_at", "created_at"}, values: []driver.Value{uuid, uuid, "stored", nil, instant}, run: func(ctx context.Context, tx *database.Tx) (string, error) {
			v, e := orm.Insert(ctx, tx, webhookstore.Secrets.Table, orm.KeySet(webhookstore.Secrets.ID, model.IDFromBytes[webhookstore.Secret]([16]byte{1, 137, 15, 71, 26, 0, 112, 0, 128, 0, 0, 0, 0, 0, 0, 1})), orm.ColumnSet(webhookstore.Secrets.EndpointID, model.IDFromBytes[webhookstore.Endpoint]([16]byte{1, 137, 15, 71, 26, 0, 112, 0, 128, 0, 0, 0, 0, 0, 0, 1})), orm.TextSet(webhookstore.Secrets.Ciphertext, "inserted"), orm.NullableTimeSetNull(webhookstore.Secrets.ExpiresAt), orm.TimeSet(webhookstore.Secrets.CreatedAt, at))
			return fingerprint(v, e)
		}},
		{name: "webhookstore_secret_write", columns: []string{"id", "endpoint_id", "ciphertext", "expires_at", "created_at"}, values: []driver.Value{uuid, uuid, "stored", nil, instant}, run: func(ctx context.Context, tx *database.Tx) (string, error) {
			v, e := orm.Update(ctx, tx, webhookstore.Secrets.Query(), webhookstore.Secrets.Key(), model.IDFromBytes[webhookstore.Secret]([16]byte{1, 137, 15, 71, 26, 0, 112, 0, 128, 0, 0, 0, 0, 0, 0, 1}), orm.TextSet(webhookstore.Secrets.Ciphertext, "updated"))
			return fingerprint(v, e)
		}},
		{name: "webhookstore_secret_identity", columns: []string{"id", "endpoint_id", "ciphertext", "expires_at", "created_at"}, values: []driver.Value{uuid, uuid, "stored", nil, instant}, run: func(ctx context.Context, tx *database.Tx) (string, error) {
			v, e := orm.Require(ctx, tx, webhookstore.Secrets.Query().Where(orm.KeyEq(webhookstore.Secrets.Key(), model.IDFromBytes[webhookstore.Secret]([16]byte{1, 137, 15, 71, 26, 0, 112, 0, 128, 0, 0, 0, 0, 0, 0, 1}))))
			if e != nil {
				return "", e
			}
			id, e := orm.IdentityOf(webhookstore.Secrets.Table, webhookstore.Secrets.Key()).ReferenceOf(v).Identity()
			return fingerprint(id, e)
		}},
		{name: "webhookstore_delivery_read", columns: []string{"id", "endpoint_id", "event", "payload", "state", "attempts", "last_status", "last_failure", "created_at", "updated_at", "delivered_at"}, values: []driver.Value{uuid, uuid, "stored", "stored", "stored", int64(1), int64(1), "stored", instant, instant, nil}, run: func(ctx context.Context, tx *database.Tx) (string, error) {
			v, e := orm.First(ctx, tx, webhookstore.Deliveries.Query().Where(orm.KeyEq(webhookstore.Deliveries.ID, model.IDFromBytes[webhookstore.Delivery]([16]byte{1, 137, 15, 71, 26, 0, 112, 0, 128, 0, 0, 0, 0, 0, 0, 1}))))
			return fingerprint(v, e)
		}},
		{name: "webhookstore_delivery_insert", columns: []string{"id", "endpoint_id", "event", "payload", "state", "attempts", "last_status", "last_failure", "created_at", "updated_at", "delivered_at"}, values: []driver.Value{uuid, uuid, "stored", "stored", "stored", int64(1), int64(1), "stored", instant, instant, nil}, run: func(ctx context.Context, tx *database.Tx) (string, error) {
			v, e := orm.Insert(ctx, tx, webhookstore.Deliveries.Table, orm.KeySet(webhookstore.Deliveries.ID, model.IDFromBytes[webhookstore.Delivery]([16]byte{1, 137, 15, 71, 26, 0, 112, 0, 128, 0, 0, 0, 0, 0, 0, 1})), orm.ColumnSet(webhookstore.Deliveries.EndpointID, model.IDFromBytes[webhookstore.Endpoint]([16]byte{1, 137, 15, 71, 26, 0, 112, 0, 128, 0, 0, 0, 0, 0, 0, 1})), orm.TextSet(webhookstore.Deliveries.Event, "inserted"), orm.TextSet(webhookstore.Deliveries.Payload, "inserted"), orm.TextSet(webhookstore.Deliveries.State, "inserted"), orm.NumberSet(webhookstore.Deliveries.Attempts, 1), orm.NumberSet(webhookstore.Deliveries.LastStatus, 1), orm.TextSet(webhookstore.Deliveries.LastFailure, "inserted"), orm.TimeSet(webhookstore.Deliveries.CreatedAt, at), orm.TimeSet(webhookstore.Deliveries.UpdatedAt, at), orm.NullableTimeSetNull(webhookstore.Deliveries.DeliveredAt))
			return fingerprint(v, e)
		}},
		{name: "webhookstore_delivery_write", columns: []string{"id", "endpoint_id", "event", "payload", "state", "attempts", "last_status", "last_failure", "created_at", "updated_at", "delivered_at"}, values: []driver.Value{uuid, uuid, "stored", "stored", "stored", int64(1), int64(1), "stored", instant, instant, nil}, run: func(ctx context.Context, tx *database.Tx) (string, error) {
			v, e := orm.Update(ctx, tx, webhookstore.Deliveries.Query(), webhookstore.Deliveries.Key(), model.IDFromBytes[webhookstore.Delivery]([16]byte{1, 137, 15, 71, 26, 0, 112, 0, 128, 0, 0, 0, 0, 0, 0, 1}), orm.TextSet(webhookstore.Deliveries.Event, "updated"))
			return fingerprint(v, e)
		}},
		{name: "webhookstore_delivery_identity", columns: []string{"id", "endpoint_id", "event", "payload", "state", "attempts", "last_status", "last_failure", "created_at", "updated_at", "delivered_at"}, values: []driver.Value{uuid, uuid, "stored", "stored", "stored", int64(1), int64(1), "stored", instant, instant, nil}, run: func(ctx context.Context, tx *database.Tx) (string, error) {
			v, e := orm.Require(ctx, tx, webhookstore.Deliveries.Query().Where(orm.KeyEq(webhookstore.Deliveries.Key(), model.IDFromBytes[webhookstore.Delivery]([16]byte{1, 137, 15, 71, 26, 0, 112, 0, 128, 0, 0, 0, 0, 0, 0, 1}))))
			if e != nil {
				return "", e
			}
			id, e := orm.IdentityOf(webhookstore.Deliveries.Table, webhookstore.Deliveries.Key()).ReferenceOf(v).Identity()
			return fingerprint(id, e)
		}},
		{name: "notificationstore_envelope_read", columns: []string{"id", "recipient", "scope", "subject_key", "identity", "origin", "name", "version", "payload", "fingerprint", "created_at"}, values: []driver.Value{uuid, "stored", "stored", "stored", `{"model":"subjects","key":{"k":"int","t":"7"}}`, `{"system":"migration","request":{}}`, "stored", int64(1), `{}`, "stored", instant}, run: func(ctx context.Context, tx *database.Tx) (string, error) {
			v, e := orm.First(ctx, tx, notificationstore.Envelopes.Query().Where(orm.KeyEq(notificationstore.Envelopes.ID, model.IDFromBytes[notificationstore.Envelope]([16]byte{1, 137, 15, 71, 26, 0, 112, 0, 128, 0, 0, 0, 0, 0, 0, 1}))))
			return fingerprint(v, e)
		}},
		{name: "notificationstore_envelope_insert", columns: []string{"id", "recipient", "scope", "subject_key", "identity", "origin", "name", "version", "payload", "fingerprint", "created_at"}, values: []driver.Value{uuid, "stored", "stored", "stored", `{"model":"subjects","key":{"k":"int","t":"7"}}`, `{"system":"migration","request":{}}`, "stored", int64(1), `{}`, "stored", instant}, run: func(ctx context.Context, tx *database.Tx) (string, error) {
			v, e := orm.Insert(ctx, tx, notificationstore.Envelopes.Table, orm.KeySet(notificationstore.Envelopes.ID, model.IDFromBytes[notificationstore.Envelope]([16]byte{1, 137, 15, 71, 26, 0, 112, 0, 128, 0, 0, 0, 0, 0, 0, 1})), orm.TextSet(notificationstore.Envelopes.Recipient, "inserted"), orm.TextSet(notificationstore.Envelopes.Scope, "inserted"), orm.TextSet(notificationstore.Envelopes.SubjectKey, "inserted"), orm.JSONSet(notificationstore.Envelopes.Identity, migrationJSON[model.Identity](t, `{"model":"subjects","key":{"k":"int","t":"7"}}`)), orm.JSONSet(notificationstore.Envelopes.Origin, migrationJSON[attribution.Origin](t, `{"system":"migration","request":{}}`)), orm.TextSet(notificationstore.Envelopes.Name, "inserted"), orm.NumberSet(notificationstore.Envelopes.Version, 1), orm.JSONSet(notificationstore.Envelopes.Payload, migrationJSON[json.RawMessage](t, `{}`)), orm.TextSet(notificationstore.Envelopes.Fingerprint, "inserted"), orm.TimeSet(notificationstore.Envelopes.CreatedAt, at))
			return fingerprint(v, e)
		}},
		{name: "notificationstore_envelope_write", columns: []string{"id", "recipient", "scope", "subject_key", "identity", "origin", "name", "version", "payload", "fingerprint", "created_at"}, values: []driver.Value{uuid, "stored", "stored", "stored", `{"model":"subjects","key":{"k":"int","t":"7"}}`, `{"system":"migration","request":{}}`, "stored", int64(1), `{}`, "stored", instant}, run: func(ctx context.Context, tx *database.Tx) (string, error) {
			v, e := orm.Update(ctx, tx, notificationstore.Envelopes.Query(), notificationstore.Envelopes.Key(), model.IDFromBytes[notificationstore.Envelope]([16]byte{1, 137, 15, 71, 26, 0, 112, 0, 128, 0, 0, 0, 0, 0, 0, 1}), orm.TextSet(notificationstore.Envelopes.Recipient, "updated"))
			return fingerprint(v, e)
		}},
		{name: "notificationstore_envelope_identity", columns: []string{"id", "recipient", "scope", "subject_key", "identity", "origin", "name", "version", "payload", "fingerprint", "created_at"}, values: []driver.Value{uuid, "stored", "stored", "stored", `{"model":"subjects","key":{"k":"int","t":"7"}}`, `{"system":"migration","request":{}}`, "stored", int64(1), `{}`, "stored", instant}, run: func(ctx context.Context, tx *database.Tx) (string, error) {
			v, e := orm.Require(ctx, tx, notificationstore.Envelopes.Query().Where(orm.KeyEq(notificationstore.Envelopes.Key(), model.IDFromBytes[notificationstore.Envelope]([16]byte{1, 137, 15, 71, 26, 0, 112, 0, 128, 0, 0, 0, 0, 0, 0, 1}))))
			if e != nil {
				return "", e
			}
			id, e := orm.IdentityOf(notificationstore.Envelopes.Table, notificationstore.Envelopes.Key()).ReferenceOf(v).Identity()
			return fingerprint(id, e)
		}},
		{name: "notificationstore_delivery_read", columns: []string{"key", "notification_id", "channel", "kind", "state", "payload", "claim", "attempts", "updated_at"}, values: []driver.Value{"stored", uuid, "stored", "stored", "stored", `{}`, uuid, int64(1), instant}, run: func(ctx context.Context, tx *database.Tx) (string, error) {
			v, e := orm.First(ctx, tx, notificationstore.Deliveries.Query().Where(orm.TextEq(notificationstore.Deliveries.KeyColumn, "stored")))
			return fingerprint(v, e)
		}},
		{name: "notificationstore_delivery_insert", columns: []string{"key", "notification_id", "channel", "kind", "state", "payload", "claim", "attempts", "updated_at"}, values: []driver.Value{"stored", uuid, "stored", "stored", "stored", `{}`, uuid, int64(1), instant}, run: func(ctx context.Context, tx *database.Tx) (string, error) {
			v, e := orm.Insert(ctx, tx, notificationstore.Deliveries.Table, orm.TextSet(notificationstore.Deliveries.KeyColumn, "stored"), orm.ColumnSet(notificationstore.Deliveries.NotificationID, model.IDFromBytes[notificationstore.Envelope]([16]byte{1, 137, 15, 71, 26, 0, 112, 0, 128, 0, 0, 0, 0, 0, 0, 1})), orm.TextSet(notificationstore.Deliveries.Channel, "inserted"), orm.TextSet(notificationstore.Deliveries.Kind, "inserted"), orm.TextSet(notificationstore.Deliveries.State, "inserted"), orm.JSONSet(notificationstore.Deliveries.Payload, migrationJSON[json.RawMessage](t, `{}`)), orm.ColumnSet(notificationstore.Deliveries.Claim, model.IDFromBytes[notificationstore.Delivery]([16]byte{1, 137, 15, 71, 26, 0, 112, 0, 128, 0, 0, 0, 0, 0, 0, 1})), orm.NumberSet(notificationstore.Deliveries.Attempts, 1), orm.TimeSet(notificationstore.Deliveries.UpdatedAt, at))
			return fingerprint(v, e)
		}},
		{name: "notificationstore_delivery_write", columns: []string{"key", "notification_id", "channel", "kind", "state", "payload", "claim", "attempts", "updated_at"}, values: []driver.Value{"stored", uuid, "stored", "stored", "stored", `{}`, uuid, int64(1), instant}, run: func(ctx context.Context, tx *database.Tx) (string, error) {
			v, e := orm.Update(ctx, tx, notificationstore.Deliveries.Query(), notificationstore.Deliveries.Key(), "stored", orm.TextSet(notificationstore.Deliveries.Channel, "updated"))
			return fingerprint(v, e)
		}},
		{name: "notificationstore_delivery_identity", columns: []string{"key", "notification_id", "channel", "kind", "state", "payload", "claim", "attempts", "updated_at"}, values: []driver.Value{"stored", uuid, "stored", "stored", "stored", `{}`, uuid, int64(1), instant}, run: func(ctx context.Context, tx *database.Tx) (string, error) {
			v, e := orm.Require(ctx, tx, notificationstore.Deliveries.Query().Where(orm.KeyEq(notificationstore.Deliveries.Key(), "stored")))
			if e != nil {
				return "", e
			}
			id, e := orm.IdentityOf(notificationstore.Deliveries.Table, notificationstore.Deliveries.Key()).ReferenceOf(v).Identity()
			return fingerprint(id, e)
		}},
		{name: "notificationstore_inbox_read", columns: []string{"id", "scope", "subject_key", "name", "version", "data", "created_at", "read_at"}, values: []driver.Value{uuid, "stored", "stored", "stored", int64(1), `{}`, instant, nil}, run: func(ctx context.Context, tx *database.Tx) (string, error) {
			v, e := orm.First(ctx, tx, notificationstore.Inboxes.Query().Where(orm.KeyEq(notificationstore.Inboxes.ID, model.IDFromBytes[notificationstore.Inbox]([16]byte{1, 137, 15, 71, 26, 0, 112, 0, 128, 0, 0, 0, 0, 0, 0, 1}))))
			return fingerprint(v, e)
		}},
		{name: "notificationstore_inbox_insert", columns: []string{"id", "scope", "subject_key", "name", "version", "data", "created_at", "read_at"}, values: []driver.Value{uuid, "stored", "stored", "stored", int64(1), `{}`, instant, nil}, run: func(ctx context.Context, tx *database.Tx) (string, error) {
			v, e := orm.Insert(ctx, tx, notificationstore.Inboxes.Table, orm.KeySet(notificationstore.Inboxes.ID, model.IDFromBytes[notificationstore.Inbox]([16]byte{1, 137, 15, 71, 26, 0, 112, 0, 128, 0, 0, 0, 0, 0, 0, 1})), orm.TextSet(notificationstore.Inboxes.Scope, "inserted"), orm.TextSet(notificationstore.Inboxes.SubjectKey, "inserted"), orm.TextSet(notificationstore.Inboxes.Name, "inserted"), orm.NumberSet(notificationstore.Inboxes.Version, 1), orm.JSONSet(notificationstore.Inboxes.Data, migrationJSON[json.RawMessage](t, `{}`)), orm.TimeSet(notificationstore.Inboxes.CreatedAt, at), orm.NullableTimeSetNull(notificationstore.Inboxes.ReadAt))
			return fingerprint(v, e)
		}},
		{name: "notificationstore_inbox_write", columns: []string{"id", "scope", "subject_key", "name", "version", "data", "created_at", "read_at"}, values: []driver.Value{uuid, "stored", "stored", "stored", int64(1), `{}`, instant, nil}, run: func(ctx context.Context, tx *database.Tx) (string, error) {
			v, e := orm.Update(ctx, tx, notificationstore.Inboxes.Query(), notificationstore.Inboxes.Key(), model.IDFromBytes[notificationstore.Inbox]([16]byte{1, 137, 15, 71, 26, 0, 112, 0, 128, 0, 0, 0, 0, 0, 0, 1}), orm.TextSet(notificationstore.Inboxes.Scope, "updated"))
			return fingerprint(v, e)
		}},
		{name: "notificationstore_inbox_identity", columns: []string{"id", "scope", "subject_key", "name", "version", "data", "created_at", "read_at"}, values: []driver.Value{uuid, "stored", "stored", "stored", int64(1), `{}`, instant, nil}, run: func(ctx context.Context, tx *database.Tx) (string, error) {
			v, e := orm.Require(ctx, tx, notificationstore.Inboxes.Query().Where(orm.KeyEq(notificationstore.Inboxes.Key(), model.IDFromBytes[notificationstore.Inbox]([16]byte{1, 137, 15, 71, 26, 0, 112, 0, 128, 0, 0, 0, 0, 0, 0, 1}))))
			if e != nil {
				return "", e
			}
			id, e := orm.IdentityOf(notificationstore.Inboxes.Table, notificationstore.Inboxes.Key()).ReferenceOf(v).Identity()
			return fingerprint(id, e)
		}},
		{name: "attachmentstore_file_read", columns: []string{"id", "owner", "scope", "subject_key", "identity", "collection", "locale", "single", "disk", "object_key", "original_name", "content_type", "size", "digest", "etag", "object_version", "width", "height", "properties", "sort_order", "state", "writer_id", "attempts", "last_failure", "created_at", "updated_at"}, values: []driver.Value{uuid, "stored", "stored", "stored", `{"model":"subjects","key":{"k":"int","t":"7"}}`, "stored", "stored", false, "stored", "stored", "stored", "stored", int64(1), "stored", "stored", "stored", int64(1), int64(1), `{}`, int64(1), "stored", uuid, int64(1), "stored", instant, instant}, run: func(ctx context.Context, tx *database.Tx) (string, error) {
			v, e := orm.First(ctx, tx, attachmentstore.Files.Query().Where(orm.KeyEq(attachmentstore.Files.ID, model.IDFromBytes[attachmentstore.File]([16]byte{1, 137, 15, 71, 26, 0, 112, 0, 128, 0, 0, 0, 0, 0, 0, 1}))))
			return fingerprint(v, e)
		}},
		{name: "attachmentstore_file_insert", columns: []string{"id", "owner", "scope", "subject_key", "identity", "collection", "locale", "single", "disk", "object_key", "original_name", "content_type", "size", "digest", "etag", "object_version", "width", "height", "properties", "sort_order", "state", "writer_id", "attempts", "last_failure", "created_at", "updated_at"}, values: []driver.Value{uuid, "stored", "stored", "stored", `{"model":"subjects","key":{"k":"int","t":"7"}}`, "stored", "stored", false, "stored", "stored", "stored", "stored", int64(1), "stored", "stored", "stored", int64(1), int64(1), `{}`, int64(1), "stored", uuid, int64(1), "stored", instant, instant}, run: func(ctx context.Context, tx *database.Tx) (string, error) {
			v, e := orm.Insert(ctx, tx, attachmentstore.Files.Table, orm.KeySet(attachmentstore.Files.ID, model.IDFromBytes[attachmentstore.File]([16]byte{1, 137, 15, 71, 26, 0, 112, 0, 128, 0, 0, 0, 0, 0, 0, 1})), orm.TextSet(attachmentstore.Files.Owner, "inserted"), orm.TextSet(attachmentstore.Files.Scope, "inserted"), orm.TextSet(attachmentstore.Files.SubjectKey, "inserted"), orm.JSONSet(attachmentstore.Files.Identity, migrationJSON[model.Identity](t, `{"model":"subjects","key":{"k":"int","t":"7"}}`)), orm.TextSet(attachmentstore.Files.Collection, "inserted"), orm.TextSet(attachmentstore.Files.Locale, "inserted"), orm.ColumnSet(attachmentstore.Files.Single, true), orm.TextSet(attachmentstore.Files.Disk, "inserted"), orm.TextSet(attachmentstore.Files.ObjectKey, "inserted"), orm.TextSet(attachmentstore.Files.OriginalName, "inserted"), orm.TextSet(attachmentstore.Files.ContentType, "inserted"), orm.NumberSet(attachmentstore.Files.Size, 1), orm.TextSet(attachmentstore.Files.Digest, "inserted"), orm.TextSet(attachmentstore.Files.ETag, "inserted"), orm.TextSet(attachmentstore.Files.ObjectVersion, "inserted"), orm.NumberSet(attachmentstore.Files.Width, 1), orm.NumberSet(attachmentstore.Files.Height, 1), orm.JSONSet(attachmentstore.Files.Properties, migrationJSON[json.RawMessage](t, `{}`)), orm.NumberSet(attachmentstore.Files.SortOrder, 1), orm.TextSet(attachmentstore.Files.State, "inserted"), orm.ColumnSet(attachmentstore.Files.WriterID, model.IDFromBytes[attachmentstore.Writer]([16]byte{1, 137, 15, 71, 26, 0, 112, 0, 128, 0, 0, 0, 0, 0, 0, 1})), orm.NumberSet(attachmentstore.Files.Attempts, 1), orm.TextSet(attachmentstore.Files.LastFailure, "inserted"), orm.TimeSet(attachmentstore.Files.CreatedAt, at), orm.TimeSet(attachmentstore.Files.UpdatedAt, at))
			return fingerprint(v, e)
		}},
		{name: "attachmentstore_file_write", columns: []string{"id", "owner", "scope", "subject_key", "identity", "collection", "locale", "single", "disk", "object_key", "original_name", "content_type", "size", "digest", "etag", "object_version", "width", "height", "properties", "sort_order", "state", "writer_id", "attempts", "last_failure", "created_at", "updated_at"}, values: []driver.Value{uuid, "stored", "stored", "stored", `{"model":"subjects","key":{"k":"int","t":"7"}}`, "stored", "stored", false, "stored", "stored", "stored", "stored", int64(1), "stored", "stored", "stored", int64(1), int64(1), `{}`, int64(1), "stored", uuid, int64(1), "stored", instant, instant}, run: func(ctx context.Context, tx *database.Tx) (string, error) {
			v, e := orm.Update(ctx, tx, attachmentstore.Files.Query(), attachmentstore.Files.Key(), model.IDFromBytes[attachmentstore.File]([16]byte{1, 137, 15, 71, 26, 0, 112, 0, 128, 0, 0, 0, 0, 0, 0, 1}), orm.TextSet(attachmentstore.Files.Owner, "updated"))
			return fingerprint(v, e)
		}},
		{name: "attachmentstore_file_identity", columns: []string{"id", "owner", "scope", "subject_key", "identity", "collection", "locale", "single", "disk", "object_key", "original_name", "content_type", "size", "digest", "etag", "object_version", "width", "height", "properties", "sort_order", "state", "writer_id", "attempts", "last_failure", "created_at", "updated_at"}, values: []driver.Value{uuid, "stored", "stored", "stored", `{"model":"subjects","key":{"k":"int","t":"7"}}`, "stored", "stored", false, "stored", "stored", "stored", "stored", int64(1), "stored", "stored", "stored", int64(1), int64(1), `{}`, int64(1), "stored", uuid, int64(1), "stored", instant, instant}, run: func(ctx context.Context, tx *database.Tx) (string, error) {
			v, e := orm.Require(ctx, tx, attachmentstore.Files.Query().Where(orm.KeyEq(attachmentstore.Files.Key(), model.IDFromBytes[attachmentstore.File]([16]byte{1, 137, 15, 71, 26, 0, 112, 0, 128, 0, 0, 0, 0, 0, 0, 1}))))
			if e != nil {
				return "", e
			}
			id, e := orm.IdentityOf(attachmentstore.Files.Table, attachmentstore.Files.Key()).ReferenceOf(v).Identity()
			return fingerprint(id, e)
		}},
		{name: "attachmentstore_variant_read", columns: []string{"id", "file_id", "name", "disk", "object_key", "content_type", "size", "digest", "etag", "object_version", "width", "height", "state", "writer_id", "attempts", "last_failure", "created_at", "updated_at"}, values: []driver.Value{uuid, uuid, "stored", "stored", "stored", "stored", int64(1), "stored", "stored", "stored", int64(1), int64(1), "stored", uuid, int64(1), "stored", instant, instant}, run: func(ctx context.Context, tx *database.Tx) (string, error) {
			v, e := orm.First(ctx, tx, attachmentstore.Variants.Query().Where(orm.KeyEq(attachmentstore.Variants.ID, model.IDFromBytes[attachmentstore.Variant]([16]byte{1, 137, 15, 71, 26, 0, 112, 0, 128, 0, 0, 0, 0, 0, 0, 1}))))
			return fingerprint(v, e)
		}},
		{name: "attachmentstore_variant_insert", columns: []string{"id", "file_id", "name", "disk", "object_key", "content_type", "size", "digest", "etag", "object_version", "width", "height", "state", "writer_id", "attempts", "last_failure", "created_at", "updated_at"}, values: []driver.Value{uuid, uuid, "stored", "stored", "stored", "stored", int64(1), "stored", "stored", "stored", int64(1), int64(1), "stored", uuid, int64(1), "stored", instant, instant}, run: func(ctx context.Context, tx *database.Tx) (string, error) {
			v, e := orm.Insert(ctx, tx, attachmentstore.Variants.Table, orm.KeySet(attachmentstore.Variants.ID, model.IDFromBytes[attachmentstore.Variant]([16]byte{1, 137, 15, 71, 26, 0, 112, 0, 128, 0, 0, 0, 0, 0, 0, 1})), orm.ColumnSet(attachmentstore.Variants.FileID, model.IDFromBytes[attachmentstore.File]([16]byte{1, 137, 15, 71, 26, 0, 112, 0, 128, 0, 0, 0, 0, 0, 0, 1})), orm.TextSet(attachmentstore.Variants.Name, "inserted"), orm.TextSet(attachmentstore.Variants.Disk, "inserted"), orm.TextSet(attachmentstore.Variants.ObjectKey, "inserted"), orm.TextSet(attachmentstore.Variants.ContentType, "inserted"), orm.NumberSet(attachmentstore.Variants.Size, 1), orm.TextSet(attachmentstore.Variants.Digest, "inserted"), orm.TextSet(attachmentstore.Variants.ETag, "inserted"), orm.TextSet(attachmentstore.Variants.ObjectVersion, "inserted"), orm.NumberSet(attachmentstore.Variants.Width, 1), orm.NumberSet(attachmentstore.Variants.Height, 1), orm.TextSet(attachmentstore.Variants.State, "inserted"), orm.ColumnSet(attachmentstore.Variants.WriterID, model.IDFromBytes[attachmentstore.Writer]([16]byte{1, 137, 15, 71, 26, 0, 112, 0, 128, 0, 0, 0, 0, 0, 0, 1})), orm.NumberSet(attachmentstore.Variants.Attempts, 1), orm.TextSet(attachmentstore.Variants.LastFailure, "inserted"), orm.TimeSet(attachmentstore.Variants.CreatedAt, at), orm.TimeSet(attachmentstore.Variants.UpdatedAt, at))
			return fingerprint(v, e)
		}},
		{name: "attachmentstore_variant_write", columns: []string{"id", "file_id", "name", "disk", "object_key", "content_type", "size", "digest", "etag", "object_version", "width", "height", "state", "writer_id", "attempts", "last_failure", "created_at", "updated_at"}, values: []driver.Value{uuid, uuid, "stored", "stored", "stored", "stored", int64(1), "stored", "stored", "stored", int64(1), int64(1), "stored", uuid, int64(1), "stored", instant, instant}, run: func(ctx context.Context, tx *database.Tx) (string, error) {
			v, e := orm.Update(ctx, tx, attachmentstore.Variants.Query(), attachmentstore.Variants.Key(), model.IDFromBytes[attachmentstore.Variant]([16]byte{1, 137, 15, 71, 26, 0, 112, 0, 128, 0, 0, 0, 0, 0, 0, 1}), orm.TextSet(attachmentstore.Variants.Name, "updated"))
			return fingerprint(v, e)
		}},
		{name: "attachmentstore_variant_identity", columns: []string{"id", "file_id", "name", "disk", "object_key", "content_type", "size", "digest", "etag", "object_version", "width", "height", "state", "writer_id", "attempts", "last_failure", "created_at", "updated_at"}, values: []driver.Value{uuid, uuid, "stored", "stored", "stored", "stored", int64(1), "stored", "stored", "stored", int64(1), int64(1), "stored", uuid, int64(1), "stored", instant, instant}, run: func(ctx context.Context, tx *database.Tx) (string, error) {
			v, e := orm.Require(ctx, tx, attachmentstore.Variants.Query().Where(orm.KeyEq(attachmentstore.Variants.Key(), model.IDFromBytes[attachmentstore.Variant]([16]byte{1, 137, 15, 71, 26, 0, 112, 0, 128, 0, 0, 0, 0, 0, 0, 1}))))
			if e != nil {
				return "", e
			}
			id, e := orm.IdentityOf(attachmentstore.Variants.Table, attachmentstore.Variants.Key()).ReferenceOf(v).Identity()
			return fingerprint(id, e)
		}},
		{name: "extensionstore_meta_read", columns: []string{"key", "owner", "scope", "subject_key", "identity", "name", "version", "value", "created_at", "updated_at"}, values: []driver.Value{"stored", "stored", "stored", "stored", `{"model":"subjects","key":{"k":"int","t":"7"}}`, "stored", int64(1), `{}`, instant, instant}, run: func(ctx context.Context, tx *database.Tx) (string, error) {
			v, e := orm.First(ctx, tx, extensionstore.Metadata.Query().Where(orm.TextEq(extensionstore.Metadata.KeyColumn, "stored")))
			return fingerprint(v, e)
		}},
		{name: "extensionstore_meta_insert", columns: []string{"key", "owner", "scope", "subject_key", "identity", "name", "version", "value", "created_at", "updated_at"}, values: []driver.Value{"stored", "stored", "stored", "stored", `{"model":"subjects","key":{"k":"int","t":"7"}}`, "stored", int64(1), `{}`, instant, instant}, run: func(ctx context.Context, tx *database.Tx) (string, error) {
			v, e := orm.Insert(ctx, tx, extensionstore.Metadata.Table, orm.TextSet(extensionstore.Metadata.KeyColumn, "stored"), orm.TextSet(extensionstore.Metadata.Owner, "inserted"), orm.TextSet(extensionstore.Metadata.Scope, "inserted"), orm.TextSet(extensionstore.Metadata.SubjectKey, "inserted"), orm.JSONSet(extensionstore.Metadata.Identity, migrationJSON[model.Identity](t, `{"model":"subjects","key":{"k":"int","t":"7"}}`)), orm.TextSet(extensionstore.Metadata.Name, "inserted"), orm.NumberSet(extensionstore.Metadata.Version, 1), orm.JSONSet(extensionstore.Metadata.Value, migrationJSON[json.RawMessage](t, `{}`)), orm.TimeSet(extensionstore.Metadata.CreatedAt, at), orm.TimeSet(extensionstore.Metadata.UpdatedAt, at))
			return fingerprint(v, e)
		}},
		{name: "extensionstore_meta_write", columns: []string{"key", "owner", "scope", "subject_key", "identity", "name", "version", "value", "created_at", "updated_at"}, values: []driver.Value{"stored", "stored", "stored", "stored", `{"model":"subjects","key":{"k":"int","t":"7"}}`, "stored", int64(1), `{}`, instant, instant}, run: func(ctx context.Context, tx *database.Tx) (string, error) {
			v, e := orm.Update(ctx, tx, extensionstore.Metadata.Query(), extensionstore.Metadata.Key(), "stored", orm.TextSet(extensionstore.Metadata.Owner, "updated"))
			return fingerprint(v, e)
		}},
		{name: "extensionstore_meta_identity", columns: []string{"key", "owner", "scope", "subject_key", "identity", "name", "version", "value", "created_at", "updated_at"}, values: []driver.Value{"stored", "stored", "stored", "stored", `{"model":"subjects","key":{"k":"int","t":"7"}}`, "stored", int64(1), `{}`, instant, instant}, run: func(ctx context.Context, tx *database.Tx) (string, error) {
			v, e := orm.Require(ctx, tx, extensionstore.Metadata.Query().Where(orm.KeyEq(extensionstore.Metadata.Key(), "stored")))
			if e != nil {
				return "", e
			}
			id, e := orm.IdentityOf(extensionstore.Metadata.Table, extensionstore.Metadata.Key()).ReferenceOf(v).Identity()
			return fingerprint(id, e)
		}},
		{name: "extensionstore_setting_read", columns: []string{"name", "version", "value", "kind", "parameters", "group_name", "label", "description", "sort_order", "is_public", "presentation_declared", "created_at", "updated_at"}, values: []driver.Value{"stored", int64(1), `{}`, "stored", `{}`, "stored", "stored", "stored", int64(1), false, false, instant, instant}, run: func(ctx context.Context, tx *database.Tx) (string, error) {
			v, e := orm.First(ctx, tx, extensionstore.Settings.Query().Where(orm.TextEq(extensionstore.Settings.Name, "stored")))
			return fingerprint(v, e)
		}},
		{name: "extensionstore_setting_insert", columns: []string{"name", "version", "value", "kind", "parameters", "group_name", "label", "description", "sort_order", "is_public", "presentation_declared", "created_at", "updated_at"}, values: []driver.Value{"stored", int64(1), `{}`, "stored", `{}`, "stored", "stored", "stored", int64(1), false, false, instant, instant}, run: func(ctx context.Context, tx *database.Tx) (string, error) {
			v, e := orm.Insert(ctx, tx, extensionstore.Settings.Table, orm.TextSet(extensionstore.Settings.Name, "stored"), orm.NumberSet(extensionstore.Settings.Version, 1), orm.JSONSet(extensionstore.Settings.Value, migrationJSON[json.RawMessage](t, `{}`)), orm.TextSet(extensionstore.Settings.Kind, "inserted"), orm.JSONSet(extensionstore.Settings.Parameters, migrationJSON[json.RawMessage](t, `{}`)), orm.TextSet(extensionstore.Settings.GroupName, "inserted"), orm.TextSet(extensionstore.Settings.Label, "inserted"), orm.TextSet(extensionstore.Settings.Description, "inserted"), orm.NumberSet(extensionstore.Settings.SortOrder, 1), orm.ColumnSet(extensionstore.Settings.IsPublic, true), orm.ColumnSet(extensionstore.Settings.PresentationDeclared, true), orm.TimeSet(extensionstore.Settings.CreatedAt, at), orm.TimeSet(extensionstore.Settings.UpdatedAt, at))
			return fingerprint(v, e)
		}},
		{name: "extensionstore_setting_write", columns: []string{"name", "version", "value", "kind", "parameters", "group_name", "label", "description", "sort_order", "is_public", "presentation_declared", "created_at", "updated_at"}, values: []driver.Value{"stored", int64(1), `{}`, "stored", `{}`, "stored", "stored", "stored", int64(1), false, false, instant, instant}, run: func(ctx context.Context, tx *database.Tx) (string, error) {
			v, e := orm.Update(ctx, tx, extensionstore.Settings.Query(), extensionstore.Settings.Key(), "stored", orm.TextSet(extensionstore.Settings.Kind, "updated"))
			return fingerprint(v, e)
		}},
		{name: "extensionstore_setting_identity", columns: []string{"name", "version", "value", "kind", "parameters", "group_name", "label", "description", "sort_order", "is_public", "presentation_declared", "created_at", "updated_at"}, values: []driver.Value{"stored", int64(1), `{}`, "stored", `{}`, "stored", "stored", "stored", int64(1), false, false, instant, instant}, run: func(ctx context.Context, tx *database.Tx) (string, error) {
			v, e := orm.Require(ctx, tx, extensionstore.Settings.Query().Where(orm.KeyEq(extensionstore.Settings.Key(), "stored")))
			if e != nil {
				return "", e
			}
			id, e := orm.IdentityOf(extensionstore.Settings.Table, extensionstore.Settings.Key()).ReferenceOf(v).Identity()
			return fingerprint(id, e)
		}},
		{name: "extensionstore_translation_read", columns: []string{"key", "owner", "scope", "subject_key", "identity", "field", "locale", "value", "created_at", "updated_at"}, values: []driver.Value{"stored", "stored", "stored", "stored", `{"model":"subjects","key":{"k":"int","t":"7"}}`, "stored", "stored", "stored", instant, instant}, run: func(ctx context.Context, tx *database.Tx) (string, error) {
			v, e := orm.First(ctx, tx, extensionstore.Translations.Query().Where(orm.TextEq(extensionstore.Translations.KeyColumn, "stored")))
			return fingerprint(v, e)
		}},
		{name: "extensionstore_translation_insert", columns: []string{"key", "owner", "scope", "subject_key", "identity", "field", "locale", "value", "created_at", "updated_at"}, values: []driver.Value{"stored", "stored", "stored", "stored", `{"model":"subjects","key":{"k":"int","t":"7"}}`, "stored", "stored", "stored", instant, instant}, run: func(ctx context.Context, tx *database.Tx) (string, error) {
			v, e := orm.Insert(ctx, tx, extensionstore.Translations.Table, orm.TextSet(extensionstore.Translations.KeyColumn, "stored"), orm.TextSet(extensionstore.Translations.Owner, "inserted"), orm.TextSet(extensionstore.Translations.Scope, "inserted"), orm.TextSet(extensionstore.Translations.SubjectKey, "inserted"), orm.JSONSet(extensionstore.Translations.Identity, migrationJSON[model.Identity](t, `{"model":"subjects","key":{"k":"int","t":"7"}}`)), orm.TextSet(extensionstore.Translations.Field, "inserted"), orm.TextSet(extensionstore.Translations.Locale, "inserted"), orm.TextSet(extensionstore.Translations.Value, "inserted"), orm.TimeSet(extensionstore.Translations.CreatedAt, at), orm.TimeSet(extensionstore.Translations.UpdatedAt, at))
			return fingerprint(v, e)
		}},
		{name: "extensionstore_translation_write", columns: []string{"key", "owner", "scope", "subject_key", "identity", "field", "locale", "value", "created_at", "updated_at"}, values: []driver.Value{"stored", "stored", "stored", "stored", `{"model":"subjects","key":{"k":"int","t":"7"}}`, "stored", "stored", "stored", instant, instant}, run: func(ctx context.Context, tx *database.Tx) (string, error) {
			v, e := orm.Update(ctx, tx, extensionstore.Translations.Query(), extensionstore.Translations.Key(), "stored", orm.TextSet(extensionstore.Translations.Owner, "updated"))
			return fingerprint(v, e)
		}},
		{name: "extensionstore_translation_identity", columns: []string{"key", "owner", "scope", "subject_key", "identity", "field", "locale", "value", "created_at", "updated_at"}, values: []driver.Value{"stored", "stored", "stored", "stored", `{"model":"subjects","key":{"k":"int","t":"7"}}`, "stored", "stored", "stored", instant, instant}, run: func(ctx context.Context, tx *database.Tx) (string, error) {
			v, e := orm.Require(ctx, tx, extensionstore.Translations.Query().Where(orm.KeyEq(extensionstore.Translations.Key(), "stored")))
			if e != nil {
				return "", e
			}
			id, e := orm.IdentityOf(extensionstore.Translations.Table, extensionstore.Translations.Key()).ReferenceOf(v).Identity()
			return fingerprint(id, e)
		}},
		{name: "auditstore_entry_read", columns: []string{"id", "sequence", "area", "operation", "action", "version", "subject", "subject_key", "payload", "redacted", "redaction", "origin", "actor_key", "correlation", "request_method", "request_route", "created_at"}, values: []driver.Value{uuid, int64(1), "stored", int64(1), "stored", int64(1), nil, nil, `{}`, false, int64(1), `{"system":"migration","request":{}}`, nil, nil, "stored", "stored", instant}, run: func(ctx context.Context, tx *database.Tx) (string, error) {
			v, e := orm.First(ctx, tx, auditstore.Entries.Query().Where(orm.KeyEq(auditstore.Entries.ID, model.IDFromBytes[auditstore.Entry]([16]byte{1, 137, 15, 71, 26, 0, 112, 0, 128, 0, 0, 0, 0, 0, 0, 1}))))
			return fingerprint(v, e)
		}},
		{name: "auditstore_entry_insert", columns: []string{"id", "sequence", "area", "operation", "action", "version", "subject", "subject_key", "payload", "redacted", "redaction", "origin", "actor_key", "correlation", "request_method", "request_route", "created_at"}, values: []driver.Value{uuid, int64(1), "stored", int64(1), "stored", int64(1), nil, nil, `{}`, false, int64(1), `{"system":"migration","request":{}}`, nil, nil, "stored", "stored", instant}, run: func(ctx context.Context, tx *database.Tx) (string, error) {
			v, e := orm.Insert(ctx, tx, auditstore.Entries.Table, orm.KeySet(auditstore.Entries.ID, model.IDFromBytes[auditstore.Entry]([16]byte{1, 137, 15, 71, 26, 0, 112, 0, 128, 0, 0, 0, 0, 0, 0, 1})), orm.TextSet(auditstore.Entries.Area, "inserted"), orm.ColumnSet(auditstore.Entries.Operation, 1), orm.TextSet(auditstore.Entries.Action, "inserted"), orm.NumberSet(auditstore.Entries.Version, 1), orm.NullableJSONSetNull(auditstore.Entries.Subject), orm.JSONSet(auditstore.Entries.Payload, migrationJSON[json.RawMessage](t, `{}`)), orm.ColumnSet(auditstore.Entries.Redacted, true), orm.NumberSet(auditstore.Entries.Redaction, 1), orm.JSONSet(auditstore.Entries.Origin, migrationJSON[attribution.Origin](t, `{"system":"migration","request":{}}`)), orm.NullableTextSetNull(auditstore.Entries.Correlation), orm.TextSet(auditstore.Entries.RequestMethod, "inserted"), orm.TextSet(auditstore.Entries.RequestRoute, "inserted"))
			return fingerprint(v, e)
		}},
		{name: "auditstore_entry_write", columns: []string{"id", "sequence", "area", "operation", "action", "version", "subject", "subject_key", "payload", "redacted", "redaction", "origin", "actor_key", "correlation", "request_method", "request_route", "created_at"}, values: []driver.Value{uuid, int64(1), "stored", int64(1), "stored", int64(1), nil, nil, `{}`, false, int64(1), `{"system":"migration","request":{}}`, nil, nil, "stored", "stored", instant}, run: func(ctx context.Context, tx *database.Tx) (string, error) {
			v, e := orm.Update(ctx, tx, auditstore.Entries.Query(), auditstore.Entries.Key(), model.IDFromBytes[auditstore.Entry]([16]byte{1, 137, 15, 71, 26, 0, 112, 0, 128, 0, 0, 0, 0, 0, 0, 1}), orm.TextSet(auditstore.Entries.Area, "updated"))
			return fingerprint(v, e)
		}},
		{name: "auditstore_entry_identity", columns: []string{"id", "sequence", "area", "operation", "action", "version", "subject", "subject_key", "payload", "redacted", "redaction", "origin", "actor_key", "correlation", "request_method", "request_route", "created_at"}, values: []driver.Value{uuid, int64(1), "stored", int64(1), "stored", int64(1), nil, nil, `{}`, false, int64(1), `{"system":"migration","request":{}}`, nil, nil, "stored", "stored", instant}, run: func(ctx context.Context, tx *database.Tx) (string, error) {
			v, e := orm.Require(ctx, tx, auditstore.Entries.Query().Where(orm.KeyEq(auditstore.Entries.Key(), model.IDFromBytes[auditstore.Entry]([16]byte{1, 137, 15, 71, 26, 0, 112, 0, 128, 0, 0, 0, 0, 0, 0, 1}))))
			if e != nil {
				return "", e
			}
			id, e := orm.IdentityOf(auditstore.Entries.Table, auditstore.Entries.Key()).ReferenceOf(v).Identity()
			return fingerprint(id, e)
		}},
		{name: "webhookstore_endpoint_uuid_seek", columns: []string{"id", "url", "events", "active", "created_at", "updated_at"}, values: []driver.Value{uuid, "stored", `[]`, false, instant, instant}, run: func(ctx context.Context, tx *database.Tx) (string, error) {
			v, e := orm.All(ctx, tx, webhookstore.Endpoints.Query().Where(orm.UUIDGreater(orm.KeyExpr(webhookstore.Endpoints.ID), model.IDFromBytes[webhookstore.Endpoint]([16]byte{1, 137, 15, 71, 26, 0, 112, 0, 128, 0, 0, 0, 0, 0, 0, 1}))).OrderBy(orm.KeyAsc(webhookstore.Endpoints.ID)).Limit(2))
			return fingerprint(v, e)
		}},
		{name: "webhookstore_secret_uuid_seek", columns: []string{"id", "endpoint_id", "ciphertext", "expires_at", "created_at"}, values: []driver.Value{uuid, uuid, "stored", nil, instant}, run: func(ctx context.Context, tx *database.Tx) (string, error) {
			v, e := orm.All(ctx, tx, webhookstore.Secrets.Query().Where(orm.UUIDGreater(orm.KeyExpr(webhookstore.Secrets.ID), model.IDFromBytes[webhookstore.Secret]([16]byte{1, 137, 15, 71, 26, 0, 112, 0, 128, 0, 0, 0, 0, 0, 0, 1}))).OrderBy(orm.KeyAsc(webhookstore.Secrets.ID)).Limit(2))
			return fingerprint(v, e)
		}},
		{name: "webhookstore_delivery_uuid_seek", columns: []string{"id", "endpoint_id", "event", "payload", "state", "attempts", "last_status", "last_failure", "created_at", "updated_at", "delivered_at"}, values: []driver.Value{uuid, uuid, "stored", "stored", "stored", int64(1), int64(1), "stored", instant, instant, nil}, run: func(ctx context.Context, tx *database.Tx) (string, error) {
			v, e := orm.All(ctx, tx, webhookstore.Deliveries.Query().Where(orm.UUIDGreater(orm.KeyExpr(webhookstore.Deliveries.ID), model.IDFromBytes[webhookstore.Delivery]([16]byte{1, 137, 15, 71, 26, 0, 112, 0, 128, 0, 0, 0, 0, 0, 0, 1}))).OrderBy(orm.KeyAsc(webhookstore.Deliveries.ID)).Limit(2))
			return fingerprint(v, e)
		}},
		{name: "notificationstore_inbox_update_each", columns: []string{"id", "scope", "subject_key", "name", "version", "data", "created_at", "read_at"}, values: []driver.Value{uuid, "stored", "stored", "stored", int64(1), `{}`, instant, nil}, run: func(ctx context.Context, tx *database.Tx) (string, error) {
			v, e := orm.UpdateEach(ctx, tx, notificationstore.Inboxes.Query().Where(orm.KeyIn(notificationstore.Inboxes.ID, model.IDFromBytes[notificationstore.Inbox]([16]byte{1, 137, 15, 71, 26, 0, 112, 0, 128, 0, 0, 0, 0, 0, 0, 1}))), 1, func(context.Context, *database.Tx, notificationstore.Inbox) ([]orm.Assignment[notificationstore.Inbox], error) {
				return []orm.Assignment[notificationstore.Inbox]{orm.NullableTimeSet(notificationstore.Inboxes.ReadAt, at)}, nil
			})
			return fingerprint(v, e)
		}},
		{name: "notificationstore_inbox_delete_each", columns: []string{"id", "scope", "subject_key", "name", "version", "data", "created_at", "read_at"}, values: []driver.Value{uuid, "stored", "stored", "stored", int64(1), `{}`, instant, nil}, run: func(ctx context.Context, tx *database.Tx) (string, error) {
			v, e := orm.DeleteEach(ctx, tx, notificationstore.Inboxes.Query().Where(orm.KeyIn(notificationstore.Inboxes.ID, model.IDFromBytes[notificationstore.Inbox]([16]byte{1, 137, 15, 71, 26, 0, 112, 0, 128, 0, 0, 0, 0, 0, 0, 1}))), 1)
			return fingerprint(v, e)
		}},
		{name: "attachmentstore_file_uuid_seek", columns: []string{"id", "owner", "scope", "subject_key", "identity", "collection", "locale", "single", "disk", "object_key", "original_name", "content_type", "size", "digest", "etag", "object_version", "width", "height", "properties", "sort_order", "state", "writer_id", "attempts", "last_failure", "created_at", "updated_at"}, values: []driver.Value{uuid, "stored", "stored", "stored", `{"model":"subjects","key":{"k":"int","t":"7"}}`, "stored", "stored", false, "stored", "stored", "stored", "stored", int64(1), "stored", "stored", "stored", int64(1), int64(1), `{}`, int64(1), "stored", uuid, int64(1), "stored", instant, instant}, run: func(ctx context.Context, tx *database.Tx) (string, error) {
			v, e := orm.All(ctx, tx, attachmentstore.Files.Query().Where(orm.UUIDGreater(orm.KeyExpr(attachmentstore.Files.ID), model.IDFromBytes[attachmentstore.File]([16]byte{1, 137, 15, 71, 26, 0, 112, 0, 128, 0, 0, 0, 0, 0, 0, 1}))).OrderBy(orm.KeyAsc(attachmentstore.Files.ID)).Limit(2))
			return fingerprint(v, e)
		}},
		{name: "attachmentstore_file_update_each", columns: []string{"id", "owner", "scope", "subject_key", "identity", "collection", "locale", "single", "disk", "object_key", "original_name", "content_type", "size", "digest", "etag", "object_version", "width", "height", "properties", "sort_order", "state", "writer_id", "attempts", "last_failure", "created_at", "updated_at"}, values: []driver.Value{uuid, "stored", "stored", "stored", `{"model":"subjects","key":{"k":"int","t":"7"}}`, "stored", "stored", false, "stored", "stored", "stored", "stored", int64(1), "stored", "stored", "stored", int64(1), int64(1), `{}`, int64(1), "stored", uuid, int64(1), "stored", instant, instant}, run: func(ctx context.Context, tx *database.Tx) (string, error) {
			v, e := orm.UpdateEach(ctx, tx, attachmentstore.Files.Query().Where(orm.KeyIn(attachmentstore.Files.ID, model.IDFromBytes[attachmentstore.File]([16]byte{1, 137, 15, 71, 26, 0, 112, 0, 128, 0, 0, 0, 0, 0, 0, 1}))), 1, func(context.Context, *database.Tx, attachmentstore.File) ([]orm.Assignment[attachmentstore.File], error) {
				return []orm.Assignment[attachmentstore.File]{orm.TextSet(attachmentstore.Files.State, "ready")}, nil
			})
			return fingerprint(v, e)
		}},
		{name: "attachmentstore_file_delete_each", columns: []string{"id", "owner", "scope", "subject_key", "identity", "collection", "locale", "single", "disk", "object_key", "original_name", "content_type", "size", "digest", "etag", "object_version", "width", "height", "properties", "sort_order", "state", "writer_id", "attempts", "last_failure", "created_at", "updated_at"}, values: []driver.Value{uuid, "stored", "stored", "stored", `{"model":"subjects","key":{"k":"int","t":"7"}}`, "stored", "stored", false, "stored", "stored", "stored", "stored", int64(1), "stored", "stored", "stored", int64(1), int64(1), `{}`, int64(1), "stored", uuid, int64(1), "stored", instant, instant}, run: func(ctx context.Context, tx *database.Tx) (string, error) {
			v, e := orm.DeleteEach(ctx, tx, attachmentstore.Files.Query().Where(orm.KeyIn(attachmentstore.Files.ID, model.IDFromBytes[attachmentstore.File]([16]byte{1, 137, 15, 71, 26, 0, 112, 0, 128, 0, 0, 0, 0, 0, 0, 1}))), 1)
			return fingerprint(v, e)
		}},
		{name: "attachmentstore_variant_uuid_seek", columns: []string{"id", "file_id", "name", "disk", "object_key", "content_type", "size", "digest", "etag", "object_version", "width", "height", "state", "writer_id", "attempts", "last_failure", "created_at", "updated_at"}, values: []driver.Value{uuid, uuid, "stored", "stored", "stored", "stored", int64(1), "stored", "stored", "stored", int64(1), int64(1), "stored", uuid, int64(1), "stored", instant, instant}, run: func(ctx context.Context, tx *database.Tx) (string, error) {
			v, e := orm.All(ctx, tx, attachmentstore.Variants.Query().Where(orm.UUIDGreater(orm.KeyExpr(attachmentstore.Variants.ID), model.IDFromBytes[attachmentstore.Variant]([16]byte{1, 137, 15, 71, 26, 0, 112, 0, 128, 0, 0, 0, 0, 0, 0, 1}))).OrderBy(orm.KeyAsc(attachmentstore.Variants.ID)).Limit(2))
			return fingerprint(v, e)
		}},
		{name: "extensionstore_translation_source_delete", columns: []string{"key", "owner", "scope", "subject_key", "identity", "field", "locale", "value", "created_at", "updated_at"}, values: []driver.Value{"stored", "stored", "stored", "stored", `{"model":"subjects","key":{"k":"int","t":"7"}}`, "stored", "stored", "stored", instant, instant}, run: func(ctx context.Context, tx *database.Tx) (string, error) {
			v, e := rows.DeleteUsing(extensionstore.Translations.Query().Where(orm.TextEq(extensionstore.Translations.Scope, "scope")), rows.From(extensionstore.Translations.Query().Where(orm.TextEq(extensionstore.Translations.Scope, "scope")).OrderBy(orm.TextAsc(extensionstore.Translations.KeyColumn)).Limit(3))).Match(rows.MatchSource(orm.TextExpr(extensionstore.Translations.KeyColumn), expr.Value(orm.TextExpr(extensionstore.Translations.KeyColumn)))).Exec(ctx, tx)
			return fingerprint(v, e)
		}},
		{name: "auditstore_entry_prune", columns: []string{"id", "sequence", "area", "operation", "action", "version", "subject", "subject_key", "payload", "redacted", "redaction", "origin", "actor_key", "correlation", "request_method", "request_route", "created_at"}, values: []driver.Value{uuid, int64(1), "stored", int64(1), "stored", int64(1), nil, nil, `{}`, false, int64(1), `{"system":"migration","request":{}}`, nil, nil, "stored", "stored", instant}, run: func(ctx context.Context, tx *database.Tx) (string, error) {
			v, e := auditstore.Prune(ctx, tx, "area", at, 3)
			return fingerprint(v, e)
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := &capture{columns: tc.columns, values: tc.values}
			db, err := database.Open(t.Context(), database.Adapter{Connector: connector{c}}, database.DefaultPoolConfig(), database.WithClock(testkit.NewClock(instant)))
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close(context.Background())
			err = db.Transaction(t.Context(), func(tx *database.Tx) error { var e error; c.Result, e = tc.run(t.Context(), tx); return e })
			if err != nil {
				t.Fatal(err)
			}
			data, e := json.MarshalIndent(c, "", "  ")
			if e != nil {
				t.Fatal(e)
			}
			data = append(data, '\n')
			path := filepath.Join("testdata", "stage7_3b_"+tc.name+".json")
			want, e := os.ReadFile(path)
			if e != nil {
				t.Fatal(e)
			}
			if !reflect.DeepEqual(data, want) {
				t.Fatalf("store transcript changed:\n%s\nwant:\n%s", data, want)
			}
		})
	}
}

func migrationJSON[T any](t *testing.T, raw string) value.JSON[T] {
	t.Helper()
	v, e := value.ParseJSON[T](raw)
	if e != nil {
		t.Fatal(e)
	}
	return v
}
