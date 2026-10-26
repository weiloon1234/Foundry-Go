package orm

import "time"

import (
	"database/sql/driver"
	"errors"
	"fmt"
	"strings"
	"testing"
	"unsafe"

	auditrecord "github.com/weiloon1234/Foundry-Go/audit/record"
	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/database/encrypted"
	"github.com/weiloon1234/Foundry-Go/database/lifecycle"
	"github.com/weiloon1234/Foundry-Go/internal/querycore"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/value"
)

type auditPreferences struct {
	Theme string `json:"theme"`
	Token string `json:"token"`
}
type auditRow struct {
	ID     int64
	Name   string
	Quiet  string
	Note   value.Nullable[string]
	Digest string
	Secret encrypted.Text
	Prefs  value.JSON[auditPreferences]
}
type auditTable struct {
	Table[auditRow, int64]
	Name   Text[auditRow, string]
	Quiet  Text[auditRow, string]
	Note   NullableText[auditRow, string]
	Digest Column[auditRow, string]
	Secret Encrypted[auditRow, encrypted.Text]
	Prefs  JSON[auditRow, auditPreferences]
}

func auditFields(bindings *int) auditTable {
	sensitive := codec.New(func(v string) (driver.Value, error) { *bindings++; return v, nil }, func(v any) (string, error) { return v.(string), nil }).WithParameterType(codec.TypeText).WithSensitiveValues()
	return Define("audit_records", func(b Builder[auditRow], m *auditRow) auditTable {
		return auditTable{TableOf(b, &m.ID), TextOf(b, &m.Name), TextOf(b, &m.Quiet), NullableTextOf(b, &m.Note), ColumnOf(b, &m.Digest, Codec(sensitive)), EncryptedTextOf(b, &m.Secret), JSONOf(b, &m.Prefs)}
	})
}
func auditChanges(t *testing.T, table auditTable, operation lifecycle.Operation, before, after *auditRow, assignments ...Assignment[auditRow]) Changes[auditRow] {
	t.Helper()
	var as []querycore.MutationAssignment
	for _, a := range assignments {
		as = append(as, a.core)
	}
	state, err := captureChanges(table.core, unsafe.Pointer(before), unsafe.Pointer(after), as, value.Set(operation))
	if err != nil {
		t.Fatal(err)
	}
	return Changes[auditRow]{state: state}
}
func TestAuditStoredSnapshotsPoliciesAndHistoricalFields(t *testing.T) {
	for _, op := range []lifecycle.Operation{lifecycle.Create, lifecycle.Update, lifecycle.SoftDelete, lifecycle.Restore, lifecycle.Delete, lifecycle.ForceDelete} {
		t.Run(fmt.Sprint(op), func(t *testing.T) {
			bindings := 0
			table := auditFields(&bindings)
			prefs, err := value.ParseJSON[auditPreferences](`{"theme":"light","token":"nested-private"}`)
			if err != nil {
				t.Fatal(err)
			}
			before := auditRow{ID: 7, Name: "stored", Quiet: "quiet", Note: value.Of("old"), Digest: "hash-private", Secret: encrypted.NewText("encrypted-private"), Prefs: prefs}
			after := before
			after.Note = value.Null[string]()
			left, right := &before, &after
			if op == lifecycle.Create {
				left = nil
			}
			if op == lifecycle.Delete || op == lifecycle.ForceDelete {
				right = nil
			}
			var as []Assignment[auditRow]
			if right != nil {
				as = []Assignment[auditRow]{table.Name.Set("stored"), table.Note.SetNull()}
			}
			changes := auditChanges(t, table, op, left, right, as...)
			bindings = 0
			policy := AuditPolicyOf(table.Table, AuditField(table.Quiet, auditrecord.Exclude))
			history, err := Audit(table.Table, changes, policy)
			if err != nil {
				t.Fatal(err)
			}
			payload, err := history.Entry().Payload()
			if err != nil {
				t.Fatal(err)
			}
			if bindings != 0 || strings.Contains(payload, "private") || strings.Contains(payload, `"name":"quiet"`) {
				t.Fatal("audit disclosed/bound protected data", bindings, payload)
			}
			subject, err := history.Subject()
			if err != nil || subject.Key() != 7 {
				t.Fatal("persisted identity", err)
			}
			n, err := AuditRead(history, table.Name)
			if err != nil {
				t.Fatal(err)
			}
			name, ok := n.Get()
			if !ok {
				t.Fatal("assigned field omitted")
			}
			if left != nil && right != nil && (!name.Assigned() || name.Changed()) {
				t.Fatal("assigned and changed conflated")
			}
			nval, err := name.After().Get()
			if err != nil {
				t.Fatal(err)
			}
			if v, ok := nval.Get(); right != nil && (!ok || v != "stored") {
				t.Fatal("stored value changed")
			}
			note, err := AuditReadNullable(history, table.Note)
			if err != nil {
				t.Fatal(err)
			}
			nv, _ := note.Get()
			v, err := nv.After().Get()
			if err != nil {
				t.Fatal(err)
			}
			if right != nil {
				n, _ := v.Get()
				if !n.IsNull() {
					t.Fatal("NULL confused with absence")
				}
			}
			digest, err := AuditRead(history, table.Digest)
			if err != nil {
				t.Fatal(err)
			}
			if left != nil && right != nil {
				if digest.IsSet() {
					t.Fatal("untouched digest retained")
				}
			} else {
				f, _ := digest.Get()
				snap := f.After()
				if right == nil {
					snap = f.Before()
				}
				if snap.State() != auditrecord.Redacted {
					t.Fatal("sensitive disclosure")
				}
				if _, err := snap.Get(); err == nil {
					t.Fatal("redaction fabricated value")
				}
			}
			secret, err := AuditReadEncrypted(history, table.Secret)
			if err != nil {
				t.Fatal(err)
			}
			if f, ok := secret.Get(); ok {
				snap := f.After()
				if right == nil {
					snap = f.Before()
				}
				if snap.State() != auditrecord.Redacted {
					t.Fatal("unsealed encryption was bound")
				}
			}
			jsonField, err := AuditRead(history, table.Prefs)
			if err != nil {
				t.Fatal(err)
			}
			if f, ok := jsonField.Get(); ok {
				snap := f.After()
				if right == nil {
					snap = f.Before()
				}
				if snap.State() != auditrecord.RedactedJSON {
					t.Fatal("JSON keys not redacted")
				}
				if _, err := snap.Get(); err == nil {
					t.Fatal("partial JSON hydrated")
				}
			}
			// Read a stored/reloaded document through the same concrete field codecs.
			entry, err := auditrecord.ParseEntry(history.Entry().Identity(), op, payload)
			if err != nil {
				t.Fatal(err)
			}
			restored, err := auditrecord.RestoreModel(subject, entry)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = AuditRead(restored, table.Key()); err != nil {
				t.Fatal(err)
			}
		})
	}
}
func TestAuditRejectsInvalidPolicyAndStandaloneChanges(t *testing.T) {
	n := 0
	table := auditFields(&n)
	other := auditFields(&n)
	m := auditRow{ID: 1}
	state := Changes[auditRow]{state: &changeState{table: table.core, after: unsafe.Pointer(&m), operation: value.Set(lifecycle.Create)}}
	for _, p := range []AuditPolicy[auditRow]{
		AuditPolicyOf(table.Table, AuditField(table.Key(), auditrecord.Exclude)),
		AuditPolicyOf(table.Table, AuditField(table.Key(), auditrecord.Redact)),
		AuditPolicyOf(table.Table, AuditField(table.Name, auditrecord.Disclosure(99))),
		AuditPolicyOf(table.Table, AuditField(other.Name, auditrecord.Automatic)),
		AuditPolicyOf(table.Table, AuditField(table.Name, auditrecord.Redact), AuditField(table.Name, auditrecord.Exclude)),
		AuditPolicyOf(other.Table),
	} {
		if _, err := Audit(table.Table, state, p); err == nil {
			t.Fatal("invalid policy accepted")
		}
	}
	state.state.operation = value.Optional[lifecycle.Operation]{}
	if _, err := Audit(table.Table, state, AuditPolicy[auditRow]{}); err == nil {
		t.Fatal("standalone comparison audited")
	}
}
func TestAuditCodecErrorsAreSafeAndDoNotRebindUntouchedFields(t *testing.T) {
	calls := 0
	table := auditFields(&calls)
	before := auditRow{ID: 1, Name: "same"}
	after := before
	state := &changeState{table: table.core, before: unsafe.Pointer(&before), after: unsafe.Pointer(&after), operation: value.Set(lifecycle.Update), fields: map[*columnCore]changeFlags{table.Name.text.core: {assigned: true}}}
	private := errors.New("private bound value")
	table.Name.text.core.get = func(unsafe.Pointer) (driver.Value, error) { return nil, private }
	table.Quiet.text.core.get = func(unsafe.Pointer) (driver.Value, error) { t.Error("untouched field rebound"); return nil, nil }
	_, err := Audit(table.Table, Changes[auditRow]{state: state}, AuditPolicy[auditRow]{})
	if !errors.Is(err, private) || strings.Contains(fmt.Sprintf("%v %+v %#v", err, err, err), private.Error()) {
		t.Fatal("unsafe codec error", err)
	}
	// Explicit redaction also suppresses an otherwise failing binder.
	if _, err = Audit(table.Table, Changes[auditRow]{state: state}, AuditPolicyOf(table.Table, AuditField(table.Name, auditrecord.Redact))); err != nil {
		t.Fatal(err)
	}
}

func TestAuditKeepsPersistedPrimaryCodec(t *testing.T) {
	type key string
	type item struct{ ID key }
	keyCodec := codec.New(func(k key) (driver.Value, error) { return "stored:" + string(k), nil }, func(v any) (key, error) {
		s, ok := v.(string)
		if !ok || !strings.HasPrefix(s, "stored:") {
			return "", errors.New("bad key")
		}
		return key(strings.TrimPrefix(s, "stored:")), nil
	}).WithParameterType(codec.TypeText)
	table := Define("audit_custom_keys", func(b Builder[item], m *item) Table[item, key] { return TableOf(b, &m.ID, Codec(keyCodec)) })
	m := item{ID: "seven"}
	state, err := captureChanges(table.core, nil, unsafe.Pointer(&m), nil, value.Set(lifecycle.Create))
	if err != nil {
		t.Fatal(err)
	}
	history, err := Audit(table, Changes[item]{state: state}, AuditPolicy[item]{})
	if err != nil {
		t.Fatal(err)
	}
	payload, err := history.Entry().Payload()
	if err != nil || !strings.Contains(payload, "stored:seven") {
		t.Fatal("identity bypassed stored codec", err)
	}
	subject, err := history.Subject()
	if err != nil || subject.Key() != m.ID {
		t.Fatal("typed subject codec", err)
	}
	field, err := AuditRead(history, table.Key())
	if err != nil {
		t.Fatal(err)
	}
	change, _ := field.Get()
	v, err := change.After().Get()
	if err != nil {
		t.Fatal(err)
	}
	k, _ := v.Get()
	if k != m.ID {
		t.Fatal("historical key codec")
	}
}

func TestAuditNullableEncryptionDoesNotBindUnsealedValues(t *testing.T) {
	table := secureRecords()
	id, err := model.NewIDAt[secureRecord](time.Now())
	if err != nil {
		t.Fatal(err)
	}
	m := secureRecord{ID: id, Secret: encrypted.NewText("unsealed private"), Backup: value.Of(encrypted.NewText("nullable private"))}
	state, err := captureChanges(table.core, nil, unsafe.Pointer(&m), nil, value.Set(lifecycle.Create))
	if err != nil {
		t.Fatal(err)
	}
	for _, policy := range []AuditPolicy[secureRecord]{AuditPolicy[secureRecord]{}, AuditPolicyOf(table.Table, AuditEncryptedField(table.Secret, auditrecord.Exclude), AuditNullableEncryptedField(table.Backup, auditrecord.Exclude))} {
		history, err := Audit(table.Table, Changes[secureRecord]{state: state}, policy)
		if err != nil {
			t.Fatal(err)
		}
		field, err := AuditReadNullableEncrypted(history, table.Backup)
		if err != nil {
			t.Fatal(err)
		}
		if change, present := field.Get(); present {
			if change.After().State() != auditrecord.Redacted {
				t.Fatal("encrypted value disclosed")
			}
			if _, err := change.After().Get(); err == nil {
				t.Fatal("redaction fabricated nullable field")
			}
		}
	}
}
