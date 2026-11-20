package orm

import (
	"testing"

	"github.com/weiloon1234/Foundry-Go/database/encrypted"
	"github.com/weiloon1234/Foundry-Go/value"
)

func BenchmarkUnifiedOperators(b *testing.B) {
	type benchRow struct {
		ID     int64
		Name   string
		Count  int64
		Note   value.Nullable[string]
		Secret encrypted.Text
		Backup value.Nullable[encrypted.Text]
	}
	type benchTable struct {
		Table[benchRow, int64]
		Name   Text[benchRow, string]
		Count  Number[benchRow, int64]
		Note   NullableText[benchRow, string]
		Secret Encrypted[benchRow, encrypted.Text]
		Backup NullableEncrypted[benchRow, encrypted.Text]
	}
	tbl := Define("bench_rows", func(builder Builder[benchRow], m *benchRow) benchTable {
		return benchTable{
			Table:  TableOf(builder, &m.ID),
			Name:   TextOf(builder, &m.Name),
			Count:  NumberOf(builder, &m.Count),
			Note:   NullableTextOf(builder, &m.Note),
			Secret: EncryptedTextOf(builder, &m.Secret),
			Backup: NullableEncryptedTextOf(builder, &m.Backup),
		}
	})
	if err := Check(tbl); err != nil {
		b.Fatal(err)
	}
	secret := encrypted.NewText("benchmark-secret")

	b.Run("Eq", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			p := Eq(tbl.Name, "ada")
			if p.err != nil {
				b.Fatal(p.err)
			}
		}
	})
	b.Run("Gt", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			p := Gt(tbl.Count, int64(42))
			if p.err != nil {
				b.Fatal(p.err)
			}
		}
	})
	b.Run("Like", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			p := Like(tbl.Name, "ada%")
			if p.err != nil {
				b.Fatal(p.err)
			}
		}
	})
	b.Run("Set", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			a := Set(tbl.Name, "ada")
			if a.err != nil {
				b.Fatal(a.err)
			}
		}
	})
	b.Run("SetNull", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			a := SetNull(tbl.Note)
			if a.err != nil {
				b.Fatal(a.err)
			}
		}
	})
	b.Run("Increment", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			a := Increment(tbl.Count, int64(1))
			if a.err != nil {
				b.Fatal(a.err)
			}
		}
	})
	b.Run("Asc", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			o := Asc(tbl.Count)
			if o.err != nil {
				b.Fatal(o.err)
			}
		}
	})
	b.Run("EncryptedSet", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			a := Set(tbl.Secret, secret)
			if a.err != nil {
				b.Fatal(a.err)
			}
		}
	})
	b.Run("EncryptedIsNull", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			p := IsNull(tbl.Backup)
			if p.err != nil {
				b.Fatal(p.err)
			}
		}
	})
}
