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
			v, e := webhookstore.Endpoints.Query().Where(webhookstore.Endpoints.ID.Eq(model.IDFromBytes[webhookstore.Endpoint]([16]byte{1, 137, 15, 71, 26, 0, 112, 0, 128, 0, 0, 0, 0, 0, 0, 1}))).First(ctx, tx)
			return fingerprint(v, e)
		}},
		{name: "webhookstore_endpoint_insert", columns: []string{"id", "url", "events", "active", "created_at", "updated_at"}, values: []driver.Value{uuid, "stored", `[]`, false, instant, instant}, run: func(ctx context.Context, tx *database.Tx) (string, error) {
			v, e := webhookstore.Endpoints.Insert(ctx, tx, webhookstore.Endpoints.ID.Set(model.IDFromBytes[webhookstore.Endpoint]([16]byte{1, 137, 15, 71, 26, 0, 112, 0, 128, 0, 0, 0, 0, 0, 0, 1})), webhookstore.Endpoints.URL.Set("inserted"), webhookstore.Endpoints.Events.Set(migrationJSON[[]string](t, `[]`)), webhookstore.Endpoints.Active.Set(true), webhookstore.Endpoints.CreatedAt.Set(at), webhookstore.Endpoints.UpdatedAt.Set(at))
			return fingerprint(v, e)
		}},
		{name: "webhookstore_endpoint_write", columns: []string{"id", "url", "events", "active", "created_at", "updated_at"}, values: []driver.Value{uuid, "stored", `[]`, false, instant, instant}, run: func(ctx context.Context, tx *database.Tx) (string, error) {
			v, e := orm.Update(ctx, tx, webhookstore.Endpoints.Query(), webhookstore.Endpoints.Key(), model.IDFromBytes[webhookstore.Endpoint]([16]byte{1, 137, 15, 71, 26, 0, 112, 0, 128, 0, 0, 0, 0, 0, 0, 1}), webhookstore.Endpoints.URL.Set("updated"))
			return fingerprint(v, e)
		}},
		{name: "webhookstore_endpoint_identity", columns: []string{"id", "url", "events", "active", "created_at", "updated_at"}, values: []driver.Value{uuid, "stored", `[]`, false, instant, instant}, run: func(ctx context.Context, tx *database.Tx) (string, error) {
			v, e := webhookstore.Endpoints.Query().Where(webhookstore.Endpoints.Key().Eq(model.IDFromBytes[webhookstore.Endpoint]([16]byte{1, 137, 15, 71, 26, 0, 112, 0, 128, 0, 0, 0, 0, 0, 0, 1}))).Require(ctx, tx)
			if e != nil {
				return "", e
			}
			id, e := orm.IdentityOf(webhookstore.Endpoints.Table, webhookstore.Endpoints.Key()).ReferenceOf(v).Identity()
			return fingerprint(id, e)
		}},
		{name: "webhookstore_secret_read", columns: []string{"id", "endpoint_id", "ciphertext", "expires_at", "created_at"}, values: []driver.Value{uuid, uuid, "stored", nil, instant}, run: func(ctx context.Context, tx *database.Tx) (string, error) {
			v, e := webhookstore.Secrets.Query().Where(webhookstore.Secrets.ID.Eq(model.IDFromBytes[webhookstore.Secret]([16]byte{1, 137, 15, 71, 26, 0, 112, 0, 128, 0, 0, 0, 0, 0, 0, 1}))).First(ctx, tx)
			return fingerprint(v, e)
		}},
		{name: "webhookstore_secret_insert", columns: []string{"id", "endpoint_id", "ciphertext", "expires_at", "created_at"}, values: []driver.Value{uuid, uuid, "stored", nil, instant}, run: func(ctx context.Context, tx *database.Tx) (string, error) {
			v, e := webhookstore.Secrets.Insert(ctx, tx, webhookstore.Secrets.ID.Set(model.IDFromBytes[webhookstore.Secret]([16]byte{1, 137, 15, 71, 26, 0, 112, 0, 128, 0, 0, 0, 0, 0, 0, 1})), webhookstore.Secrets.EndpointID.Set(model.IDFromBytes[webhookstore.Endpoint]([16]byte{1, 137, 15, 71, 26, 0, 112, 0, 128, 0, 0, 0, 0, 0, 0, 1})), webhookstore.Secrets.Ciphertext.Set("inserted"), webhookstore.Secrets.ExpiresAt.SetNull(), webhookstore.Secrets.CreatedAt.Set(at))
			return fingerprint(v, e)
		}},
		{name: "webhookstore_secret_write", columns: []string{"id", "endpoint_id", "ciphertext", "expires_at", "created_at"}, values: []driver.Value{uuid, uuid, "stored", nil, instant}, run: func(ctx context.Context, tx *database.Tx) (string, error) {
			v, e := orm.Update(ctx, tx, webhookstore.Secrets.Query(), webhookstore.Secrets.Key(), model.IDFromBytes[webhookstore.Secret]([16]byte{1, 137, 15, 71, 26, 0, 112, 0, 128, 0, 0, 0, 0, 0, 0, 1}), webhookstore.Secrets.Ciphertext.Set("updated"))
			return fingerprint(v, e)
		}},
		{name: "webhookstore_secret_identity", columns: []string{"id", "endpoint_id", "ciphertext", "expires_at", "created_at"}, values: []driver.Value{uuid, uuid, "stored", nil, instant}, run: func(ctx context.Context, tx *database.Tx) (string, error) {
			v, e := webhookstore.Secrets.Query().Where(webhookstore.Secrets.Key().Eq(model.IDFromBytes[webhookstore.Secret]([16]byte{1, 137, 15, 71, 26, 0, 112, 0, 128, 0, 0, 0, 0, 0, 0, 1}))).Require(ctx, tx)
			if e != nil {
				return "", e
			}
			id, e := orm.IdentityOf(webhookstore.Secrets.Table, webhookstore.Secrets.Key()).ReferenceOf(v).Identity()
			return fingerprint(id, e)
		}},
		{name: "webhookstore_delivery_read", columns: []string{"id", "endpoint_id", "event", "payload", "state", "attempts", "last_status", "last_failure", "created_at", "updated_at", "delivered_at"}, values: []driver.Value{uuid, uuid, "stored", "stored", "stored", int64(1), int64(1), "stored", instant, instant, nil}, run: func(ctx context.Context, tx *database.Tx) (string, error) {
			v, e := webhookstore.Deliveries.Query().Where(webhookstore.Deliveries.ID.Eq(model.IDFromBytes[webhookstore.Delivery]([16]byte{1, 137, 15, 71, 26, 0, 112, 0, 128, 0, 0, 0, 0, 0, 0, 1}))).First(ctx, tx)
			return fingerprint(v, e)
		}},
		{name: "webhookstore_delivery_insert", columns: []string{"id", "endpoint_id", "event", "payload", "state", "attempts", "last_status", "last_failure", "created_at", "updated_at", "delivered_at"}, values: []driver.Value{uuid, uuid, "stored", "stored", "stored", int64(1), int64(1), "stored", instant, instant, nil}, run: func(ctx context.Context, tx *database.Tx) (string, error) {
			v, e := webhookstore.Deliveries.Insert(ctx, tx, webhookstore.Deliveries.ID.Set(model.IDFromBytes[webhookstore.Delivery]([16]byte{1, 137, 15, 71, 26, 0, 112, 0, 128, 0, 0, 0, 0, 0, 0, 1})), webhookstore.Deliveries.EndpointID.Set(model.IDFromBytes[webhookstore.Endpoint]([16]byte{1, 137, 15, 71, 26, 0, 112, 0, 128, 0, 0, 0, 0, 0, 0, 1})), webhookstore.Deliveries.Event.Set("inserted"), webhookstore.Deliveries.Payload.Set("inserted"), webhookstore.Deliveries.State.Set("inserted"), webhookstore.Deliveries.Attempts.Set(1), webhookstore.Deliveries.LastStatus.Set(1), webhookstore.Deliveries.LastFailure.Set("inserted"), webhookstore.Deliveries.CreatedAt.Set(at), webhookstore.Deliveries.UpdatedAt.Set(at), webhookstore.Deliveries.DeliveredAt.SetNull())
			return fingerprint(v, e)
		}},
		{name: "webhookstore_delivery_write", columns: []string{"id", "endpoint_id", "event", "payload", "state", "attempts", "last_status", "last_failure", "created_at", "updated_at", "delivered_at"}, values: []driver.Value{uuid, uuid, "stored", "stored", "stored", int64(1), int64(1), "stored", instant, instant, nil}, run: func(ctx context.Context, tx *database.Tx) (string, error) {
			v, e := orm.Update(ctx, tx, webhookstore.Deliveries.Query(), webhookstore.Deliveries.Key(), model.IDFromBytes[webhookstore.Delivery]([16]byte{1, 137, 15, 71, 26, 0, 112, 0, 128, 0, 0, 0, 0, 0, 0, 1}), webhookstore.Deliveries.Event.Set("updated"))
			return fingerprint(v, e)
		}},
		{name: "webhookstore_delivery_identity", columns: []string{"id", "endpoint_id", "event", "payload", "state", "attempts", "last_status", "last_failure", "created_at", "updated_at", "delivered_at"}, values: []driver.Value{uuid, uuid, "stored", "stored", "stored", int64(1), int64(1), "stored", instant, instant, nil}, run: func(ctx context.Context, tx *database.Tx) (string, error) {
			v, e := webhookstore.Deliveries.Query().Where(webhookstore.Deliveries.Key().Eq(model.IDFromBytes[webhookstore.Delivery]([16]byte{1, 137, 15, 71, 26, 0, 112, 0, 128, 0, 0, 0, 0, 0, 0, 1}))).Require(ctx, tx)
			if e != nil {
				return "", e
			}
			id, e := orm.IdentityOf(webhookstore.Deliveries.Table, webhookstore.Deliveries.Key()).ReferenceOf(v).Identity()
			return fingerprint(id, e)
		}},
		{name: "notificationstore_envelope_read", columns: []string{"id", "recipient", "scope", "subject_key", "identity", "origin", "name", "version", "payload", "fingerprint", "created_at"}, values: []driver.Value{uuid, "stored", "stored", "stored", `{"model":"subjects","key":{"k":"int","t":"7"}}`, `{"system":"migration","request":{}}`, "stored", int64(1), `{}`, "stored", instant}, run: func(ctx context.Context, tx *database.Tx) (string, error) {
			v, e := notificationstore.Envelopes.Query().Where(notificationstore.Envelopes.ID.Eq(model.IDFromBytes[notificationstore.Envelope]([16]byte{1, 137, 15, 71, 26, 0, 112, 0, 128, 0, 0, 0, 0, 0, 0, 1}))).First(ctx, tx)
			return fingerprint(v, e)
		}},
		{name: "notificationstore_envelope_insert", columns: []string{"id", "recipient", "scope", "subject_key", "identity", "origin", "name", "version", "payload", "fingerprint", "created_at"}, values: []driver.Value{uuid, "stored", "stored", "stored", `{"model":"subjects","key":{"k":"int","t":"7"}}`, `{"system":"migration","request":{}}`, "stored", int64(1), `{}`, "stored", instant}, run: func(ctx context.Context, tx *database.Tx) (string, error) {
			v, e := notificationstore.Envelopes.Insert(ctx, tx, notificationstore.Envelopes.ID.Set(model.IDFromBytes[notificationstore.Envelope]([16]byte{1, 137, 15, 71, 26, 0, 112, 0, 128, 0, 0, 0, 0, 0, 0, 1})), notificationstore.Envelopes.Recipient.Set("inserted"), notificationstore.Envelopes.Scope.Set("inserted"), notificationstore.Envelopes.SubjectKey.Set("inserted"), notificationstore.Envelopes.Identity.Set(migrationJSON[model.Identity](t, `{"model":"subjects","key":{"k":"int","t":"7"}}`)), notificationstore.Envelopes.Origin.Set(migrationJSON[attribution.Origin](t, `{"system":"migration","request":{}}`)), notificationstore.Envelopes.Name.Set("inserted"), notificationstore.Envelopes.Version.Set(1), notificationstore.Envelopes.Payload.Set(migrationJSON[json.RawMessage](t, `{}`)), notificationstore.Envelopes.Fingerprint.Set("inserted"), notificationstore.Envelopes.CreatedAt.Set(at))
			return fingerprint(v, e)
		}},
		{name: "notificationstore_envelope_write", columns: []string{"id", "recipient", "scope", "subject_key", "identity", "origin", "name", "version", "payload", "fingerprint", "created_at"}, values: []driver.Value{uuid, "stored", "stored", "stored", `{"model":"subjects","key":{"k":"int","t":"7"}}`, `{"system":"migration","request":{}}`, "stored", int64(1), `{}`, "stored", instant}, run: func(ctx context.Context, tx *database.Tx) (string, error) {
			v, e := orm.Update(ctx, tx, notificationstore.Envelopes.Query(), notificationstore.Envelopes.Key(), model.IDFromBytes[notificationstore.Envelope]([16]byte{1, 137, 15, 71, 26, 0, 112, 0, 128, 0, 0, 0, 0, 0, 0, 1}), notificationstore.Envelopes.Recipient.Set("updated"))
			return fingerprint(v, e)
		}},
		{name: "notificationstore_envelope_identity", columns: []string{"id", "recipient", "scope", "subject_key", "identity", "origin", "name", "version", "payload", "fingerprint", "created_at"}, values: []driver.Value{uuid, "stored", "stored", "stored", `{"model":"subjects","key":{"k":"int","t":"7"}}`, `{"system":"migration","request":{}}`, "stored", int64(1), `{}`, "stored", instant}, run: func(ctx context.Context, tx *database.Tx) (string, error) {
			v, e := notificationstore.Envelopes.Query().Where(notificationstore.Envelopes.Key().Eq(model.IDFromBytes[notificationstore.Envelope]([16]byte{1, 137, 15, 71, 26, 0, 112, 0, 128, 0, 0, 0, 0, 0, 0, 1}))).Require(ctx, tx)
			if e != nil {
				return "", e
			}
			id, e := orm.IdentityOf(notificationstore.Envelopes.Table, notificationstore.Envelopes.Key()).ReferenceOf(v).Identity()
			return fingerprint(id, e)
		}},
		{name: "notificationstore_delivery_read", columns: []string{"key", "notification_id", "channel", "kind", "state", "payload", "claim", "attempts", "updated_at"}, values: []driver.Value{"stored", uuid, "stored", "stored", "stored", `{}`, uuid, int64(1), instant}, run: func(ctx context.Context, tx *database.Tx) (string, error) {
			v, e := notificationstore.Deliveries.Query().Where(notificationstore.Deliveries.KeyColumn.Eq("stored")).First(ctx, tx)
			return fingerprint(v, e)
		}},
		{name: "notificationstore_delivery_insert", columns: []string{"key", "notification_id", "channel", "kind", "state", "payload", "claim", "attempts", "updated_at"}, values: []driver.Value{"stored", uuid, "stored", "stored", "stored", `{}`, uuid, int64(1), instant}, run: func(ctx context.Context, tx *database.Tx) (string, error) {
			v, e := notificationstore.Deliveries.Insert(ctx, tx, notificationstore.Deliveries.KeyColumn.Set("stored"), notificationstore.Deliveries.NotificationID.Set(model.IDFromBytes[notificationstore.Envelope]([16]byte{1, 137, 15, 71, 26, 0, 112, 0, 128, 0, 0, 0, 0, 0, 0, 1})), notificationstore.Deliveries.Channel.Set("inserted"), notificationstore.Deliveries.Kind.Set("inserted"), notificationstore.Deliveries.State.Set("inserted"), notificationstore.Deliveries.Payload.Set(migrationJSON[json.RawMessage](t, `{}`)), notificationstore.Deliveries.Claim.Set(model.IDFromBytes[notificationstore.Delivery]([16]byte{1, 137, 15, 71, 26, 0, 112, 0, 128, 0, 0, 0, 0, 0, 0, 1})), notificationstore.Deliveries.Attempts.Set(1), notificationstore.Deliveries.UpdatedAt.Set(at))
			return fingerprint(v, e)
		}},
		{name: "notificationstore_delivery_write", columns: []string{"key", "notification_id", "channel", "kind", "state", "payload", "claim", "attempts", "updated_at"}, values: []driver.Value{"stored", uuid, "stored", "stored", "stored", `{}`, uuid, int64(1), instant}, run: func(ctx context.Context, tx *database.Tx) (string, error) {
			v, e := orm.Update(ctx, tx, notificationstore.Deliveries.Query(), notificationstore.Deliveries.Key(), "stored", notificationstore.Deliveries.Channel.Set("updated"))
			return fingerprint(v, e)
		}},
		{name: "notificationstore_delivery_identity", columns: []string{"key", "notification_id", "channel", "kind", "state", "payload", "claim", "attempts", "updated_at"}, values: []driver.Value{"stored", uuid, "stored", "stored", "stored", `{}`, uuid, int64(1), instant}, run: func(ctx context.Context, tx *database.Tx) (string, error) {
			v, e := notificationstore.Deliveries.Query().Where(notificationstore.Deliveries.Key().Eq("stored")).Require(ctx, tx)
			if e != nil {
				return "", e
			}
			id, e := orm.IdentityOf(notificationstore.Deliveries.Table, notificationstore.Deliveries.Key()).ReferenceOf(v).Identity()
			return fingerprint(id, e)
		}},
		{name: "notificationstore_inbox_read", columns: []string{"id", "scope", "subject_key", "name", "version", "data", "created_at", "read_at"}, values: []driver.Value{uuid, "stored", "stored", "stored", int64(1), `{}`, instant, nil}, run: func(ctx context.Context, tx *database.Tx) (string, error) {
			v, e := notificationstore.Inboxes.Query().Where(notificationstore.Inboxes.ID.Eq(model.IDFromBytes[notificationstore.Inbox]([16]byte{1, 137, 15, 71, 26, 0, 112, 0, 128, 0, 0, 0, 0, 0, 0, 1}))).First(ctx, tx)
			return fingerprint(v, e)
		}},
		{name: "notificationstore_inbox_insert", columns: []string{"id", "scope", "subject_key", "name", "version", "data", "created_at", "read_at"}, values: []driver.Value{uuid, "stored", "stored", "stored", int64(1), `{}`, instant, nil}, run: func(ctx context.Context, tx *database.Tx) (string, error) {
			v, e := notificationstore.Inboxes.Insert(ctx, tx, notificationstore.Inboxes.ID.Set(model.IDFromBytes[notificationstore.Inbox]([16]byte{1, 137, 15, 71, 26, 0, 112, 0, 128, 0, 0, 0, 0, 0, 0, 1})), notificationstore.Inboxes.Scope.Set("inserted"), notificationstore.Inboxes.SubjectKey.Set("inserted"), notificationstore.Inboxes.Name.Set("inserted"), notificationstore.Inboxes.Version.Set(1), notificationstore.Inboxes.Data.Set(migrationJSON[json.RawMessage](t, `{}`)), notificationstore.Inboxes.CreatedAt.Set(at), notificationstore.Inboxes.ReadAt.SetNull())
			return fingerprint(v, e)
		}},
		{name: "notificationstore_inbox_write", columns: []string{"id", "scope", "subject_key", "name", "version", "data", "created_at", "read_at"}, values: []driver.Value{uuid, "stored", "stored", "stored", int64(1), `{}`, instant, nil}, run: func(ctx context.Context, tx *database.Tx) (string, error) {
			v, e := orm.Update(ctx, tx, notificationstore.Inboxes.Query(), notificationstore.Inboxes.Key(), model.IDFromBytes[notificationstore.Inbox]([16]byte{1, 137, 15, 71, 26, 0, 112, 0, 128, 0, 0, 0, 0, 0, 0, 1}), notificationstore.Inboxes.Scope.Set("updated"))
			return fingerprint(v, e)
		}},
		{name: "notificationstore_inbox_identity", columns: []string{"id", "scope", "subject_key", "name", "version", "data", "created_at", "read_at"}, values: []driver.Value{uuid, "stored", "stored", "stored", int64(1), `{}`, instant, nil}, run: func(ctx context.Context, tx *database.Tx) (string, error) {
			v, e := notificationstore.Inboxes.Query().Where(notificationstore.Inboxes.Key().Eq(model.IDFromBytes[notificationstore.Inbox]([16]byte{1, 137, 15, 71, 26, 0, 112, 0, 128, 0, 0, 0, 0, 0, 0, 1}))).Require(ctx, tx)
			if e != nil {
				return "", e
			}
			id, e := orm.IdentityOf(notificationstore.Inboxes.Table, notificationstore.Inboxes.Key()).ReferenceOf(v).Identity()
			return fingerprint(id, e)
		}},
		{name: "attachmentstore_file_read", columns: []string{"id", "owner", "scope", "subject_key", "identity", "collection", "locale", "single", "disk", "object_key", "original_name", "content_type", "size", "digest", "etag", "object_version", "width", "height", "properties", "sort_order", "state", "writer_id", "attempts", "last_failure", "created_at", "updated_at"}, values: []driver.Value{uuid, "stored", "stored", "stored", `{"model":"subjects","key":{"k":"int","t":"7"}}`, "stored", "stored", false, "stored", "stored", "stored", "stored", int64(1), "stored", "stored", "stored", int64(1), int64(1), `{}`, int64(1), "stored", uuid, int64(1), "stored", instant, instant}, run: func(ctx context.Context, tx *database.Tx) (string, error) {
			v, e := attachmentstore.Files.Query().Where(attachmentstore.Files.ID.Eq(model.IDFromBytes[attachmentstore.File]([16]byte{1, 137, 15, 71, 26, 0, 112, 0, 128, 0, 0, 0, 0, 0, 0, 1}))).First(ctx, tx)
			return fingerprint(v, e)
		}},
		{name: "attachmentstore_file_insert", columns: []string{"id", "owner", "scope", "subject_key", "identity", "collection", "locale", "single", "disk", "object_key", "original_name", "content_type", "size", "digest", "etag", "object_version", "width", "height", "properties", "sort_order", "state", "writer_id", "attempts", "last_failure", "created_at", "updated_at"}, values: []driver.Value{uuid, "stored", "stored", "stored", `{"model":"subjects","key":{"k":"int","t":"7"}}`, "stored", "stored", false, "stored", "stored", "stored", "stored", int64(1), "stored", "stored", "stored", int64(1), int64(1), `{}`, int64(1), "stored", uuid, int64(1), "stored", instant, instant}, run: func(ctx context.Context, tx *database.Tx) (string, error) {
			v, e := attachmentstore.Files.Insert(ctx, tx, attachmentstore.Files.ID.Set(model.IDFromBytes[attachmentstore.File]([16]byte{1, 137, 15, 71, 26, 0, 112, 0, 128, 0, 0, 0, 0, 0, 0, 1})), attachmentstore.Files.Owner.Set("inserted"), attachmentstore.Files.Scope.Set("inserted"), attachmentstore.Files.SubjectKey.Set("inserted"), attachmentstore.Files.Identity.Set(migrationJSON[model.Identity](t, `{"model":"subjects","key":{"k":"int","t":"7"}}`)), attachmentstore.Files.Collection.Set("inserted"), attachmentstore.Files.Locale.Set("inserted"), attachmentstore.Files.Single.Set(true), attachmentstore.Files.Disk.Set("inserted"), attachmentstore.Files.ObjectKey.Set("inserted"), attachmentstore.Files.OriginalName.Set("inserted"), attachmentstore.Files.ContentType.Set("inserted"), attachmentstore.Files.Size.Set(1), attachmentstore.Files.Digest.Set("inserted"), attachmentstore.Files.ETag.Set("inserted"), attachmentstore.Files.ObjectVersion.Set("inserted"), attachmentstore.Files.Width.Set(1), attachmentstore.Files.Height.Set(1), attachmentstore.Files.Properties.Set(migrationJSON[json.RawMessage](t, `{}`)), attachmentstore.Files.SortOrder.Set(1), attachmentstore.Files.State.Set("inserted"), attachmentstore.Files.WriterID.Set(model.IDFromBytes[attachmentstore.Writer]([16]byte{1, 137, 15, 71, 26, 0, 112, 0, 128, 0, 0, 0, 0, 0, 0, 1})), attachmentstore.Files.Attempts.Set(1), attachmentstore.Files.LastFailure.Set("inserted"), attachmentstore.Files.CreatedAt.Set(at), attachmentstore.Files.UpdatedAt.Set(at))
			return fingerprint(v, e)
		}},
		{name: "attachmentstore_file_write", columns: []string{"id", "owner", "scope", "subject_key", "identity", "collection", "locale", "single", "disk", "object_key", "original_name", "content_type", "size", "digest", "etag", "object_version", "width", "height", "properties", "sort_order", "state", "writer_id", "attempts", "last_failure", "created_at", "updated_at"}, values: []driver.Value{uuid, "stored", "stored", "stored", `{"model":"subjects","key":{"k":"int","t":"7"}}`, "stored", "stored", false, "stored", "stored", "stored", "stored", int64(1), "stored", "stored", "stored", int64(1), int64(1), `{}`, int64(1), "stored", uuid, int64(1), "stored", instant, instant}, run: func(ctx context.Context, tx *database.Tx) (string, error) {
			v, e := orm.Update(ctx, tx, attachmentstore.Files.Query(), attachmentstore.Files.Key(), model.IDFromBytes[attachmentstore.File]([16]byte{1, 137, 15, 71, 26, 0, 112, 0, 128, 0, 0, 0, 0, 0, 0, 1}), attachmentstore.Files.Owner.Set("updated"))
			return fingerprint(v, e)
		}},
		{name: "attachmentstore_file_identity", columns: []string{"id", "owner", "scope", "subject_key", "identity", "collection", "locale", "single", "disk", "object_key", "original_name", "content_type", "size", "digest", "etag", "object_version", "width", "height", "properties", "sort_order", "state", "writer_id", "attempts", "last_failure", "created_at", "updated_at"}, values: []driver.Value{uuid, "stored", "stored", "stored", `{"model":"subjects","key":{"k":"int","t":"7"}}`, "stored", "stored", false, "stored", "stored", "stored", "stored", int64(1), "stored", "stored", "stored", int64(1), int64(1), `{}`, int64(1), "stored", uuid, int64(1), "stored", instant, instant}, run: func(ctx context.Context, tx *database.Tx) (string, error) {
			v, e := attachmentstore.Files.Query().Where(attachmentstore.Files.Key().Eq(model.IDFromBytes[attachmentstore.File]([16]byte{1, 137, 15, 71, 26, 0, 112, 0, 128, 0, 0, 0, 0, 0, 0, 1}))).Require(ctx, tx)
			if e != nil {
				return "", e
			}
			id, e := orm.IdentityOf(attachmentstore.Files.Table, attachmentstore.Files.Key()).ReferenceOf(v).Identity()
			return fingerprint(id, e)
		}},
		{name: "attachmentstore_variant_read", columns: []string{"id", "file_id", "name", "disk", "object_key", "content_type", "size", "digest", "etag", "object_version", "width", "height", "state", "writer_id", "attempts", "last_failure", "created_at", "updated_at"}, values: []driver.Value{uuid, uuid, "stored", "stored", "stored", "stored", int64(1), "stored", "stored", "stored", int64(1), int64(1), "stored", uuid, int64(1), "stored", instant, instant}, run: func(ctx context.Context, tx *database.Tx) (string, error) {
			v, e := attachmentstore.Variants.Query().Where(attachmentstore.Variants.ID.Eq(model.IDFromBytes[attachmentstore.Variant]([16]byte{1, 137, 15, 71, 26, 0, 112, 0, 128, 0, 0, 0, 0, 0, 0, 1}))).First(ctx, tx)
			return fingerprint(v, e)
		}},
		{name: "attachmentstore_variant_insert", columns: []string{"id", "file_id", "name", "disk", "object_key", "content_type", "size", "digest", "etag", "object_version", "width", "height", "state", "writer_id", "attempts", "last_failure", "created_at", "updated_at"}, values: []driver.Value{uuid, uuid, "stored", "stored", "stored", "stored", int64(1), "stored", "stored", "stored", int64(1), int64(1), "stored", uuid, int64(1), "stored", instant, instant}, run: func(ctx context.Context, tx *database.Tx) (string, error) {
			v, e := attachmentstore.Variants.Insert(ctx, tx, attachmentstore.Variants.ID.Set(model.IDFromBytes[attachmentstore.Variant]([16]byte{1, 137, 15, 71, 26, 0, 112, 0, 128, 0, 0, 0, 0, 0, 0, 1})), attachmentstore.Variants.FileID.Set(model.IDFromBytes[attachmentstore.File]([16]byte{1, 137, 15, 71, 26, 0, 112, 0, 128, 0, 0, 0, 0, 0, 0, 1})), attachmentstore.Variants.Name.Set("inserted"), attachmentstore.Variants.Disk.Set("inserted"), attachmentstore.Variants.ObjectKey.Set("inserted"), attachmentstore.Variants.ContentType.Set("inserted"), attachmentstore.Variants.Size.Set(1), attachmentstore.Variants.Digest.Set("inserted"), attachmentstore.Variants.ETag.Set("inserted"), attachmentstore.Variants.ObjectVersion.Set("inserted"), attachmentstore.Variants.Width.Set(1), attachmentstore.Variants.Height.Set(1), attachmentstore.Variants.State.Set("inserted"), attachmentstore.Variants.WriterID.Set(model.IDFromBytes[attachmentstore.Writer]([16]byte{1, 137, 15, 71, 26, 0, 112, 0, 128, 0, 0, 0, 0, 0, 0, 1})), attachmentstore.Variants.Attempts.Set(1), attachmentstore.Variants.LastFailure.Set("inserted"), attachmentstore.Variants.CreatedAt.Set(at), attachmentstore.Variants.UpdatedAt.Set(at))
			return fingerprint(v, e)
		}},
		{name: "attachmentstore_variant_write", columns: []string{"id", "file_id", "name", "disk", "object_key", "content_type", "size", "digest", "etag", "object_version", "width", "height", "state", "writer_id", "attempts", "last_failure", "created_at", "updated_at"}, values: []driver.Value{uuid, uuid, "stored", "stored", "stored", "stored", int64(1), "stored", "stored", "stored", int64(1), int64(1), "stored", uuid, int64(1), "stored", instant, instant}, run: func(ctx context.Context, tx *database.Tx) (string, error) {
			v, e := orm.Update(ctx, tx, attachmentstore.Variants.Query(), attachmentstore.Variants.Key(), model.IDFromBytes[attachmentstore.Variant]([16]byte{1, 137, 15, 71, 26, 0, 112, 0, 128, 0, 0, 0, 0, 0, 0, 1}), attachmentstore.Variants.Name.Set("updated"))
			return fingerprint(v, e)
		}},
		{name: "attachmentstore_variant_identity", columns: []string{"id", "file_id", "name", "disk", "object_key", "content_type", "size", "digest", "etag", "object_version", "width", "height", "state", "writer_id", "attempts", "last_failure", "created_at", "updated_at"}, values: []driver.Value{uuid, uuid, "stored", "stored", "stored", "stored", int64(1), "stored", "stored", "stored", int64(1), int64(1), "stored", uuid, int64(1), "stored", instant, instant}, run: func(ctx context.Context, tx *database.Tx) (string, error) {
			v, e := attachmentstore.Variants.Query().Where(attachmentstore.Variants.Key().Eq(model.IDFromBytes[attachmentstore.Variant]([16]byte{1, 137, 15, 71, 26, 0, 112, 0, 128, 0, 0, 0, 0, 0, 0, 1}))).Require(ctx, tx)
			if e != nil {
				return "", e
			}
			id, e := orm.IdentityOf(attachmentstore.Variants.Table, attachmentstore.Variants.Key()).ReferenceOf(v).Identity()
			return fingerprint(id, e)
		}},
		{name: "extensionstore_meta_read", columns: []string{"key", "owner", "scope", "subject_key", "identity", "name", "version", "value", "created_at", "updated_at"}, values: []driver.Value{"stored", "stored", "stored", "stored", `{"model":"subjects","key":{"k":"int","t":"7"}}`, "stored", int64(1), `{}`, instant, instant}, run: func(ctx context.Context, tx *database.Tx) (string, error) {
			v, e := extensionstore.Metadata.Query().Where(extensionstore.Metadata.KeyColumn.Eq("stored")).First(ctx, tx)
			return fingerprint(v, e)
		}},
		{name: "extensionstore_meta_insert", columns: []string{"key", "owner", "scope", "subject_key", "identity", "name", "version", "value", "created_at", "updated_at"}, values: []driver.Value{"stored", "stored", "stored", "stored", `{"model":"subjects","key":{"k":"int","t":"7"}}`, "stored", int64(1), `{}`, instant, instant}, run: func(ctx context.Context, tx *database.Tx) (string, error) {
			v, e := extensionstore.Metadata.Insert(ctx, tx, extensionstore.Metadata.KeyColumn.Set("stored"), extensionstore.Metadata.Owner.Set("inserted"), extensionstore.Metadata.Scope.Set("inserted"), extensionstore.Metadata.SubjectKey.Set("inserted"), extensionstore.Metadata.Identity.Set(migrationJSON[model.Identity](t, `{"model":"subjects","key":{"k":"int","t":"7"}}`)), extensionstore.Metadata.Name.Set("inserted"), extensionstore.Metadata.Version.Set(1), extensionstore.Metadata.Value.Set(migrationJSON[json.RawMessage](t, `{}`)), extensionstore.Metadata.CreatedAt.Set(at), extensionstore.Metadata.UpdatedAt.Set(at))
			return fingerprint(v, e)
		}},
		{name: "extensionstore_meta_write", columns: []string{"key", "owner", "scope", "subject_key", "identity", "name", "version", "value", "created_at", "updated_at"}, values: []driver.Value{"stored", "stored", "stored", "stored", `{"model":"subjects","key":{"k":"int","t":"7"}}`, "stored", int64(1), `{}`, instant, instant}, run: func(ctx context.Context, tx *database.Tx) (string, error) {
			v, e := orm.Update(ctx, tx, extensionstore.Metadata.Query(), extensionstore.Metadata.Key(), "stored", extensionstore.Metadata.Owner.Set("updated"))
			return fingerprint(v, e)
		}},
		{name: "extensionstore_meta_identity", columns: []string{"key", "owner", "scope", "subject_key", "identity", "name", "version", "value", "created_at", "updated_at"}, values: []driver.Value{"stored", "stored", "stored", "stored", `{"model":"subjects","key":{"k":"int","t":"7"}}`, "stored", int64(1), `{}`, instant, instant}, run: func(ctx context.Context, tx *database.Tx) (string, error) {
			v, e := extensionstore.Metadata.Query().Where(extensionstore.Metadata.Key().Eq("stored")).Require(ctx, tx)
			if e != nil {
				return "", e
			}
			id, e := orm.IdentityOf(extensionstore.Metadata.Table, extensionstore.Metadata.Key()).ReferenceOf(v).Identity()
			return fingerprint(id, e)
		}},
		{name: "extensionstore_setting_read", columns: []string{"name", "version", "value", "kind", "parameters", "group_name", "label", "description", "sort_order", "is_public", "presentation_declared", "created_at", "updated_at"}, values: []driver.Value{"stored", int64(1), `{}`, "stored", `{}`, "stored", "stored", "stored", int64(1), false, false, instant, instant}, run: func(ctx context.Context, tx *database.Tx) (string, error) {
			v, e := extensionstore.Settings.Query().Where(extensionstore.Settings.Name.Eq("stored")).First(ctx, tx)
			return fingerprint(v, e)
		}},
		{name: "extensionstore_setting_insert", columns: []string{"name", "version", "value", "kind", "parameters", "group_name", "label", "description", "sort_order", "is_public", "presentation_declared", "created_at", "updated_at"}, values: []driver.Value{"stored", int64(1), `{}`, "stored", `{}`, "stored", "stored", "stored", int64(1), false, false, instant, instant}, run: func(ctx context.Context, tx *database.Tx) (string, error) {
			v, e := extensionstore.Settings.Insert(ctx, tx, extensionstore.Settings.Name.Set("stored"), extensionstore.Settings.Version.Set(1), extensionstore.Settings.Value.Set(migrationJSON[json.RawMessage](t, `{}`)), extensionstore.Settings.Kind.Set("inserted"), extensionstore.Settings.Parameters.Set(migrationJSON[json.RawMessage](t, `{}`)), extensionstore.Settings.GroupName.Set("inserted"), extensionstore.Settings.Label.Set("inserted"), extensionstore.Settings.Description.Set("inserted"), extensionstore.Settings.SortOrder.Set(1), extensionstore.Settings.IsPublic.Set(true), extensionstore.Settings.PresentationDeclared.Set(true), extensionstore.Settings.CreatedAt.Set(at), extensionstore.Settings.UpdatedAt.Set(at))
			return fingerprint(v, e)
		}},
		{name: "extensionstore_setting_write", columns: []string{"name", "version", "value", "kind", "parameters", "group_name", "label", "description", "sort_order", "is_public", "presentation_declared", "created_at", "updated_at"}, values: []driver.Value{"stored", int64(1), `{}`, "stored", `{}`, "stored", "stored", "stored", int64(1), false, false, instant, instant}, run: func(ctx context.Context, tx *database.Tx) (string, error) {
			v, e := orm.Update(ctx, tx, extensionstore.Settings.Query(), extensionstore.Settings.Key(), "stored", extensionstore.Settings.Kind.Set("updated"))
			return fingerprint(v, e)
		}},
		{name: "extensionstore_setting_identity", columns: []string{"name", "version", "value", "kind", "parameters", "group_name", "label", "description", "sort_order", "is_public", "presentation_declared", "created_at", "updated_at"}, values: []driver.Value{"stored", int64(1), `{}`, "stored", `{}`, "stored", "stored", "stored", int64(1), false, false, instant, instant}, run: func(ctx context.Context, tx *database.Tx) (string, error) {
			v, e := extensionstore.Settings.Query().Where(extensionstore.Settings.Key().Eq("stored")).Require(ctx, tx)
			if e != nil {
				return "", e
			}
			id, e := orm.IdentityOf(extensionstore.Settings.Table, extensionstore.Settings.Key()).ReferenceOf(v).Identity()
			return fingerprint(id, e)
		}},
		{name: "extensionstore_translation_read", columns: []string{"key", "owner", "scope", "subject_key", "identity", "field", "locale", "value", "created_at", "updated_at"}, values: []driver.Value{"stored", "stored", "stored", "stored", `{"model":"subjects","key":{"k":"int","t":"7"}}`, "stored", "stored", "stored", instant, instant}, run: func(ctx context.Context, tx *database.Tx) (string, error) {
			v, e := extensionstore.Translations.Query().Where(extensionstore.Translations.KeyColumn.Eq("stored")).First(ctx, tx)
			return fingerprint(v, e)
		}},
		{name: "extensionstore_translation_insert", columns: []string{"key", "owner", "scope", "subject_key", "identity", "field", "locale", "value", "created_at", "updated_at"}, values: []driver.Value{"stored", "stored", "stored", "stored", `{"model":"subjects","key":{"k":"int","t":"7"}}`, "stored", "stored", "stored", instant, instant}, run: func(ctx context.Context, tx *database.Tx) (string, error) {
			v, e := extensionstore.Translations.Insert(ctx, tx, extensionstore.Translations.KeyColumn.Set("stored"), extensionstore.Translations.Owner.Set("inserted"), extensionstore.Translations.Scope.Set("inserted"), extensionstore.Translations.SubjectKey.Set("inserted"), extensionstore.Translations.Identity.Set(migrationJSON[model.Identity](t, `{"model":"subjects","key":{"k":"int","t":"7"}}`)), extensionstore.Translations.Field.Set("inserted"), extensionstore.Translations.Locale.Set("inserted"), extensionstore.Translations.Value.Set("inserted"), extensionstore.Translations.CreatedAt.Set(at), extensionstore.Translations.UpdatedAt.Set(at))
			return fingerprint(v, e)
		}},
		{name: "extensionstore_translation_write", columns: []string{"key", "owner", "scope", "subject_key", "identity", "field", "locale", "value", "created_at", "updated_at"}, values: []driver.Value{"stored", "stored", "stored", "stored", `{"model":"subjects","key":{"k":"int","t":"7"}}`, "stored", "stored", "stored", instant, instant}, run: func(ctx context.Context, tx *database.Tx) (string, error) {
			v, e := orm.Update(ctx, tx, extensionstore.Translations.Query(), extensionstore.Translations.Key(), "stored", extensionstore.Translations.Owner.Set("updated"))
			return fingerprint(v, e)
		}},
		{name: "extensionstore_translation_identity", columns: []string{"key", "owner", "scope", "subject_key", "identity", "field", "locale", "value", "created_at", "updated_at"}, values: []driver.Value{"stored", "stored", "stored", "stored", `{"model":"subjects","key":{"k":"int","t":"7"}}`, "stored", "stored", "stored", instant, instant}, run: func(ctx context.Context, tx *database.Tx) (string, error) {
			v, e := extensionstore.Translations.Query().Where(extensionstore.Translations.Key().Eq("stored")).Require(ctx, tx)
			if e != nil {
				return "", e
			}
			id, e := orm.IdentityOf(extensionstore.Translations.Table, extensionstore.Translations.Key()).ReferenceOf(v).Identity()
			return fingerprint(id, e)
		}},
		{name: "auditstore_entry_read", columns: []string{"id", "sequence", "area", "operation", "action", "version", "subject", "subject_key", "payload", "redacted", "redaction", "origin", "actor_key", "correlation", "request_method", "request_route", "created_at"}, values: []driver.Value{uuid, int64(1), "stored", int64(1), "stored", int64(1), nil, nil, `{}`, false, int64(1), `{"system":"migration","request":{}}`, nil, nil, "stored", "stored", instant}, run: func(ctx context.Context, tx *database.Tx) (string, error) {
			v, e := auditstore.Entries.Query().Where(auditstore.Entries.ID.Eq(model.IDFromBytes[auditstore.Entry]([16]byte{1, 137, 15, 71, 26, 0, 112, 0, 128, 0, 0, 0, 0, 0, 0, 1}))).First(ctx, tx)
			return fingerprint(v, e)
		}},
		{name: "auditstore_entry_insert", columns: []string{"id", "sequence", "area", "operation", "action", "version", "subject", "subject_key", "payload", "redacted", "redaction", "origin", "actor_key", "correlation", "request_method", "request_route", "created_at"}, values: []driver.Value{uuid, int64(1), "stored", int64(1), "stored", int64(1), nil, nil, `{}`, false, int64(1), `{"system":"migration","request":{}}`, nil, nil, "stored", "stored", instant}, run: func(ctx context.Context, tx *database.Tx) (string, error) {
			v, e := auditstore.Entries.Insert(ctx, tx, auditstore.Entries.ID.Set(model.IDFromBytes[auditstore.Entry]([16]byte{1, 137, 15, 71, 26, 0, 112, 0, 128, 0, 0, 0, 0, 0, 0, 1})), auditstore.Entries.Area.Set("inserted"), auditstore.Entries.Operation.Set(1), auditstore.Entries.Action.Set("inserted"), auditstore.Entries.Version.Set(1), auditstore.Entries.Subject.SetNull(), auditstore.Entries.Payload.Set(migrationJSON[json.RawMessage](t, `{}`)), auditstore.Entries.Redacted.Set(true), auditstore.Entries.Redaction.Set(1), auditstore.Entries.Origin.Set(migrationJSON[attribution.Origin](t, `{"system":"migration","request":{}}`)), auditstore.Entries.Correlation.SetNull(), auditstore.Entries.RequestMethod.Set("inserted"), auditstore.Entries.RequestRoute.Set("inserted"))
			return fingerprint(v, e)
		}},
		{name: "auditstore_entry_write", columns: []string{"id", "sequence", "area", "operation", "action", "version", "subject", "subject_key", "payload", "redacted", "redaction", "origin", "actor_key", "correlation", "request_method", "request_route", "created_at"}, values: []driver.Value{uuid, int64(1), "stored", int64(1), "stored", int64(1), nil, nil, `{}`, false, int64(1), `{"system":"migration","request":{}}`, nil, nil, "stored", "stored", instant}, run: func(ctx context.Context, tx *database.Tx) (string, error) {
			v, e := orm.Update(ctx, tx, auditstore.Entries.Query(), auditstore.Entries.Key(), model.IDFromBytes[auditstore.Entry]([16]byte{1, 137, 15, 71, 26, 0, 112, 0, 128, 0, 0, 0, 0, 0, 0, 1}), auditstore.Entries.Area.Set("updated"))
			return fingerprint(v, e)
		}},
		{name: "auditstore_entry_identity", columns: []string{"id", "sequence", "area", "operation", "action", "version", "subject", "subject_key", "payload", "redacted", "redaction", "origin", "actor_key", "correlation", "request_method", "request_route", "created_at"}, values: []driver.Value{uuid, int64(1), "stored", int64(1), "stored", int64(1), nil, nil, `{}`, false, int64(1), `{"system":"migration","request":{}}`, nil, nil, "stored", "stored", instant}, run: func(ctx context.Context, tx *database.Tx) (string, error) {
			v, e := auditstore.Entries.Query().Where(auditstore.Entries.Key().Eq(model.IDFromBytes[auditstore.Entry]([16]byte{1, 137, 15, 71, 26, 0, 112, 0, 128, 0, 0, 0, 0, 0, 0, 1}))).Require(ctx, tx)
			if e != nil {
				return "", e
			}
			id, e := orm.IdentityOf(auditstore.Entries.Table, auditstore.Entries.Key()).ReferenceOf(v).Identity()
			return fingerprint(id, e)
		}},
		{name: "webhookstore_endpoint_uuid_seek", columns: []string{"id", "url", "events", "active", "created_at", "updated_at"}, values: []driver.Value{uuid, "stored", `[]`, false, instant, instant}, run: func(ctx context.Context, tx *database.Tx) (string, error) {
			v, e := webhookstore.Endpoints.Query().Where(orm.UUIDGreater(webhookstore.Endpoints.ID, model.IDFromBytes[webhookstore.Endpoint]([16]byte{1, 137, 15, 71, 26, 0, 112, 0, 128, 0, 0, 0, 0, 0, 0, 1}))).OrderBy(webhookstore.Endpoints.ID.Asc()).Limit(2).All(ctx, tx)
			return fingerprint(v, e)
		}},
		{name: "webhookstore_secret_uuid_seek", columns: []string{"id", "endpoint_id", "ciphertext", "expires_at", "created_at"}, values: []driver.Value{uuid, uuid, "stored", nil, instant}, run: func(ctx context.Context, tx *database.Tx) (string, error) {
			v, e := webhookstore.Secrets.Query().Where(orm.UUIDGreater(webhookstore.Secrets.ID, model.IDFromBytes[webhookstore.Secret]([16]byte{1, 137, 15, 71, 26, 0, 112, 0, 128, 0, 0, 0, 0, 0, 0, 1}))).OrderBy(webhookstore.Secrets.ID.Asc()).Limit(2).All(ctx, tx)
			return fingerprint(v, e)
		}},
		{name: "webhookstore_delivery_uuid_seek", columns: []string{"id", "endpoint_id", "event", "payload", "state", "attempts", "last_status", "last_failure", "created_at", "updated_at", "delivered_at"}, values: []driver.Value{uuid, uuid, "stored", "stored", "stored", int64(1), int64(1), "stored", instant, instant, nil}, run: func(ctx context.Context, tx *database.Tx) (string, error) {
			v, e := webhookstore.Deliveries.Query().Where(orm.UUIDGreater(webhookstore.Deliveries.ID, model.IDFromBytes[webhookstore.Delivery]([16]byte{1, 137, 15, 71, 26, 0, 112, 0, 128, 0, 0, 0, 0, 0, 0, 1}))).OrderBy(webhookstore.Deliveries.ID.Asc()).Limit(2).All(ctx, tx)
			return fingerprint(v, e)
		}},
		{name: "notificationstore_inbox_update_each", columns: []string{"id", "scope", "subject_key", "name", "version", "data", "created_at", "read_at"}, values: []driver.Value{uuid, "stored", "stored", "stored", int64(1), `{}`, instant, nil}, run: func(ctx context.Context, tx *database.Tx) (string, error) {
			v, e := orm.UpdateEach(ctx, tx, notificationstore.Inboxes.Query().Where(notificationstore.Inboxes.ID.In(model.IDFromBytes[notificationstore.Inbox]([16]byte{1, 137, 15, 71, 26, 0, 112, 0, 128, 0, 0, 0, 0, 0, 0, 1}))), 1, func(context.Context, *database.Tx, notificationstore.Inbox) ([]orm.Assignment[notificationstore.Inbox], error) {
				return []orm.Assignment[notificationstore.Inbox]{notificationstore.Inboxes.ReadAt.Set(at)}, nil
			})
			return fingerprint(v, e)
		}},
		{name: "notificationstore_inbox_delete_each", columns: []string{"id", "scope", "subject_key", "name", "version", "data", "created_at", "read_at"}, values: []driver.Value{uuid, "stored", "stored", "stored", int64(1), `{}`, instant, nil}, run: func(ctx context.Context, tx *database.Tx) (string, error) {
			v, e := orm.DeleteEach(ctx, tx, notificationstore.Inboxes.Query().Where(notificationstore.Inboxes.ID.In(model.IDFromBytes[notificationstore.Inbox]([16]byte{1, 137, 15, 71, 26, 0, 112, 0, 128, 0, 0, 0, 0, 0, 0, 1}))), 1)
			return fingerprint(v, e)
		}},
		{name: "attachmentstore_file_uuid_seek", columns: []string{"id", "owner", "scope", "subject_key", "identity", "collection", "locale", "single", "disk", "object_key", "original_name", "content_type", "size", "digest", "etag", "object_version", "width", "height", "properties", "sort_order", "state", "writer_id", "attempts", "last_failure", "created_at", "updated_at"}, values: []driver.Value{uuid, "stored", "stored", "stored", `{"model":"subjects","key":{"k":"int","t":"7"}}`, "stored", "stored", false, "stored", "stored", "stored", "stored", int64(1), "stored", "stored", "stored", int64(1), int64(1), `{}`, int64(1), "stored", uuid, int64(1), "stored", instant, instant}, run: func(ctx context.Context, tx *database.Tx) (string, error) {
			v, e := attachmentstore.Files.Query().Where(orm.UUIDGreater(attachmentstore.Files.ID, model.IDFromBytes[attachmentstore.File]([16]byte{1, 137, 15, 71, 26, 0, 112, 0, 128, 0, 0, 0, 0, 0, 0, 1}))).OrderBy(attachmentstore.Files.ID.Asc()).Limit(2).All(ctx, tx)
			return fingerprint(v, e)
		}},
		{name: "attachmentstore_file_update_each", columns: []string{"id", "owner", "scope", "subject_key", "identity", "collection", "locale", "single", "disk", "object_key", "original_name", "content_type", "size", "digest", "etag", "object_version", "width", "height", "properties", "sort_order", "state", "writer_id", "attempts", "last_failure", "created_at", "updated_at"}, values: []driver.Value{uuid, "stored", "stored", "stored", `{"model":"subjects","key":{"k":"int","t":"7"}}`, "stored", "stored", false, "stored", "stored", "stored", "stored", int64(1), "stored", "stored", "stored", int64(1), int64(1), `{}`, int64(1), "stored", uuid, int64(1), "stored", instant, instant}, run: func(ctx context.Context, tx *database.Tx) (string, error) {
			v, e := orm.UpdateEach(ctx, tx, attachmentstore.Files.Query().Where(attachmentstore.Files.ID.In(model.IDFromBytes[attachmentstore.File]([16]byte{1, 137, 15, 71, 26, 0, 112, 0, 128, 0, 0, 0, 0, 0, 0, 1}))), 1, func(context.Context, *database.Tx, attachmentstore.File) ([]orm.Assignment[attachmentstore.File], error) {
				return []orm.Assignment[attachmentstore.File]{attachmentstore.Files.State.Set("ready")}, nil
			})
			return fingerprint(v, e)
		}},
		{name: "attachmentstore_file_delete_each", columns: []string{"id", "owner", "scope", "subject_key", "identity", "collection", "locale", "single", "disk", "object_key", "original_name", "content_type", "size", "digest", "etag", "object_version", "width", "height", "properties", "sort_order", "state", "writer_id", "attempts", "last_failure", "created_at", "updated_at"}, values: []driver.Value{uuid, "stored", "stored", "stored", `{"model":"subjects","key":{"k":"int","t":"7"}}`, "stored", "stored", false, "stored", "stored", "stored", "stored", int64(1), "stored", "stored", "stored", int64(1), int64(1), `{}`, int64(1), "stored", uuid, int64(1), "stored", instant, instant}, run: func(ctx context.Context, tx *database.Tx) (string, error) {
			v, e := orm.DeleteEach(ctx, tx, attachmentstore.Files.Query().Where(attachmentstore.Files.ID.In(model.IDFromBytes[attachmentstore.File]([16]byte{1, 137, 15, 71, 26, 0, 112, 0, 128, 0, 0, 0, 0, 0, 0, 1}))), 1)
			return fingerprint(v, e)
		}},
		{name: "attachmentstore_variant_uuid_seek", columns: []string{"id", "file_id", "name", "disk", "object_key", "content_type", "size", "digest", "etag", "object_version", "width", "height", "state", "writer_id", "attempts", "last_failure", "created_at", "updated_at"}, values: []driver.Value{uuid, uuid, "stored", "stored", "stored", "stored", int64(1), "stored", "stored", "stored", int64(1), int64(1), "stored", uuid, int64(1), "stored", instant, instant}, run: func(ctx context.Context, tx *database.Tx) (string, error) {
			v, e := attachmentstore.Variants.Query().Where(orm.UUIDGreater(attachmentstore.Variants.ID, model.IDFromBytes[attachmentstore.Variant]([16]byte{1, 137, 15, 71, 26, 0, 112, 0, 128, 0, 0, 0, 0, 0, 0, 1}))).OrderBy(attachmentstore.Variants.ID.Asc()).Limit(2).All(ctx, tx)
			return fingerprint(v, e)
		}},
		{name: "extensionstore_translation_source_delete", columns: []string{"key", "owner", "scope", "subject_key", "identity", "field", "locale", "value", "created_at", "updated_at"}, values: []driver.Value{"stored", "stored", "stored", "stored", `{"model":"subjects","key":{"k":"int","t":"7"}}`, "stored", "stored", "stored", instant, instant}, run: func(ctx context.Context, tx *database.Tx) (string, error) {
			v, e := rows.DeleteUsing(extensionstore.Translations.Query().Where(extensionstore.Translations.Scope.Eq("scope")), rows.From(extensionstore.Translations.Query().Where(extensionstore.Translations.Scope.Eq("scope")).OrderBy(extensionstore.Translations.KeyColumn.Asc()).Limit(3))).Match(rows.MatchSource(extensionstore.Translations.KeyColumn, expr.Value(extensionstore.Translations.KeyColumn))).Exec(ctx, tx)
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
