package generate

import (
	"bytes"
	"fmt"
	"github.com/weiloon1234/Foundry-Go/internal/modelmeta"
	"go/types"
	"strconv"
	"strings"
)

func emitModels(p *packageInput, declarations []modelDeclaration) ([]byte, error) {
	e := newEmitter(p)
	for _, d := range declarations {
		e.reserved[d.variable] = true
		e.reserved[d.tableType] = true
		e.reserved["foundry"+d.name+"Fields"] = true
		if d.identity {
			e.reserved[d.identityVar] = true
		}
	}
	orm := e.use(framework + "/database/orm")
	for _, d := range declarations {
		e.line("// %s binds the declared fields of %s.", d.tableType, d.name)
		e.line("type %s struct {", d.tableType)
		var key, deleted modelField
		fields := make(map[string]modelField)
		for _, f := range d.fields {
			if f.carrier == "SoftDelete" {
				deleted = f
			}
			if f.key {
				key = f
			}
			if f.member != "" {
				fields[f.member] = f
			}
		}
		keyType := e.typeName(key.field.Type())
		for _, name := range d.members {
			field := fields[name]
			if name == "Table" {
				field = key
			} else if name == "Soft" && d.hasSoft {
				field = deleted
			}
			e.emitFieldHelp(field.help)
			e.line("// See [%s.%s].", d.name, field.field.Name())
			switch name {
			case "Table":
				e.line("%s.Table[%s, %s]", orm, d.name, keyType)
			case "Soft":
				if d.hasSoft {
					e.line("%s %s.SoftDeletable[%s]", name, orm, d.name)
				} else {
					e.line("%s %s", name, modelFieldType(e, orm, d, fields[name]))
				}
			default:
				e.line("%s %s", name, modelFieldType(e, orm, d, fields[name]))
			}
		}
		e.line("}")
		// Reserve locals against source types (including private models and inputs).
		builder, model, table := e.localName("b"), e.localName("m"), e.localName("table")
		e.line("// %s is the %s table.", d.variable, d.table)
		e.line("var %s = %s.Define(%q, func(%s %s.Builder[%s], %s *%s) %s {", d.variable, orm, d.table, builder, orm, d.name, model, d.name, d.tableType)
		codec := ""
		if key.tag.Codec != "" {
			codec = fmt.Sprintf(", %s.Codec(%s())", orm, key.tag.Codec)
		}
		e.line("%s := %s.TableOf(%s, &%s.%s%s)", table, orm, builder, model, d.key, codec)
		values := map[string]string{"Table": table}
		for i, f := range d.fields {
			expr := ""
			if f.key {
				if f.member == "" {
					continue
				}
				expr = table + ".Key()"
				switch f.tag.KeyRole {
				case "primary", "primary-number", "primary-text":
					expr = fmt.Sprintf("%s.PrimaryOf(%s, %s)", orm, builder, expr)
				case "input":
					expr = fmt.Sprintf("%s.InputKeyOf[%s, %s](%s, %s, %s)", orm, d.name, e.typeName(f.input), builder, expr, modelMutator(f))
				}
				switch f.tag.KeyRole {
				case "number", "primary-number":
					expr = fmt.Sprintf("%s.NumberKey(%s)", orm, expr)
				case "text", "primary-text":
					expr = fmt.Sprintf("%s.TextKey(%s)", orm, expr)
				}
			} else {
				fn := orm + "." + f.constructor
				if f.input != nil {
					fn += fmt.Sprintf("[%s, %s]", d.name, e.typeName(f.input))
				}
				expr = fmt.Sprintf("%s(%s, &%s.%s", fn, builder, model, f.field.Name())
				if f.input != nil {
					expr += ", " + modelMutator(f)
				}
				if f.tag.Codec != "" {
					expr += fmt.Sprintf(", %s.Codec(%s())", orm, f.tag.Codec)
				} else if f.intervalColumn {
					expr += fmt.Sprintf(", %s.Codec(%s.Interval())", orm, e.use(framework+"/database/codec"))
				}

				expr += ")"
			}
			if f.member == "" && f.carrier != "SoftDelete" {
				e.line("%s", expr)
				continue
			}
			local := e.localName(fmt.Sprintf("field%d", i))
			e.line("%s := %s", local, expr)
			if f.member != "" {
				values[f.member] = local
			}
			if f.carrier == "SoftDelete" && d.hasSoft {
				values["Soft"] = fmt.Sprintf("%s.SoftDeletes(%s)", orm, local)
			}
		}
		e.line("return %s{", d.tableType)
		for _, name := range d.members {
			e.line("%s: %s,", name, values[name])
		}
		e.line("}")
		tags := make([]modelmeta.FieldTag, d.shape.NumFields())
		for i := range tags {
			tags[i] = modelmeta.FieldTag{Name: d.shape.Field(i).Name(), Tag: d.shape.Tag(i)}
		}
		options := []string{fmt.Sprintf("%s.ModelFingerprint(%q)", orm, modelmeta.Fingerprint(tags))}
		if d.noTimestamps {
			options = append(options, orm+".NoTimestamps()")
		}
		if d.noSoftDeletes {
			options = append(options, orm+".NoSoftDeletes()")
		}
		if d.hooks != "" {
			options = append(options, orm+".WithHooks("+d.hooks+")")
		}
		if d.retrieval != "" {
			options = append(options, orm+".WithRetrievalHooks("+d.retrieval+")")
		}
		if d.scopes != "" {
			options = append(options, orm+".WithGlobalScopes("+d.scopes+")")
		}
		suffix := ""
		if len(options) > 0 {
			suffix = ", " + strings.Join(options, ", ")
		}
		e.line("}%s)", suffix)
		if d.identity {
			modelPackage := e.use(framework + "/model")
			e.line("var %s = %s.IdentityOf(%s.Table, %s.Key())", d.identityVar, orm, d.variable, d.variable)
			e.line("// FoundryReference retains the table's concrete stored key.")
			e.line("func (%s %s) FoundryReference() %s.Reference[%s, %s] { return %s.ReferenceOf(%s) }", model, d.name, modelPackage, d.name, keyType, d.identityVar, model)
			e.line("// FoundryIdentity returns the model's persisted identity.")
			e.line("func (%s %s) FoundryIdentity() (%s.Identity, error) { return %s.FoundryReference().Identity() }", model, d.name, modelPackage, model)
		}
		shape := "foundry" + d.name + "Fields"
		e.line("type %s struct {", shape)
		for i := 0; i < d.shape.NumFields(); i++ {
			f := d.shape.Field(i)
			tag := ""
			if d.shape.Tag(i) != "" {
				tag = " " + strconv.Quote(d.shape.Tag(i))
			}
			if f.Embedded() {
				e.line("%s%s", e.typeName(f.Type()), tag)
			} else {
				e.line("%s %s%s", f.Name(), e.typeName(f.Type()), tag)
			}
		}
		e.line("}")
		e.line("var _ = %s(%s{}) // Struct changed: run foundry generate.", shape, d.name)
		for _, f := range d.fields {
			if f.method == nil {
				continue
			}
			receiver := e.typeName(f.method.Type().(*types.Signature).Recv().Type())
			e.line("var _ func(%s, %s) (%s, error) = (%s).%s", receiver, e.typeName(f.input), e.typeName(f.stored), receiver, f.method.Name())
		}
		e.line("")
	}
	data, err := e.finish(declarations[0].position.Filename)
	if err != nil {
		return nil, err
	}
	return bytes.Replace(data, []byte(generatedHeader), []byte(generatedHeader+"\n// Directive and field comment changes are checked only by foundry generate --check."), 1), nil
}
func modelMutator(f modelField) string {
	if f.tag.Mutate != "" {
		return f.tag.Mutate
	}
	return "nil"
}
