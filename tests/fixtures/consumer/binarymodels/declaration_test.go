package binarymodels

import "github.com/weiloon1234/Foundry-Go/database/orm"

func declareRecords(options ...orm.Option) RecordTable {
	return orm.Define("binary_records", func(b orm.Builder[Record], m *Record) RecordTable {
		return RecordTable{orm.TableOf(b, &m.ID), orm.InputBytesOf[Record, Input](b, &m.Body, nil), orm.BytesOf(b, &m.Raw), orm.NullableInputBytesOf[Record, Input](b, &m.Note, nil), orm.InputTextOf[Record, Input](b, &m.Encoded, nil)}
	}, options...)
}
