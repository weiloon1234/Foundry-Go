package querycore

import (
	"strings"

	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/fault"
)

type ScalarOperation uint8

const (
	AddOperation ScalarOperation = iota + 1
	SubtractOperation
	MultiplyOperation
	DivideOperation
	RemainderOperation
	NegateOperation
	AbsOperation
	CastOperation
	LowerOperation
	UpperOperation
	TrimOperation
	TrimLeftOperation
	TrimRightOperation
	LengthOperation
	OctetLengthOperation
	ConcatOperation
	ConcatWSOperation
	SubstringOperation
	ReplaceOperation
	ExtractYearOperation
	ExtractMonthOperation
	ExtractDayOperation
	ExtractDayOfWeekOperation
	ExtractISODayOfWeekOperation
	ExtractDayOfYearOperation
	ExtractISOWeekOperation
	ExtractISOYearOperation
	ExtractQuarterOperation
	ExtractCenturyOperation
	ExtractDecadeOperation
	ExtractMillenniumOperation
	ExtractJulianDayOperation
	ExtractHourOperation
	ExtractMinuteOperation
	ExtractSecondOperation
	ExtractMillisecondsOperation
	ExtractMicrosecondsOperation
	ExtractUnixSecondsOperation
	ExtractUnixMillisecondsOperation
	FromUnixMillisOperation
	TruncateDateOperation
	TruncateLocalOperation
	TruncateInstantOperation
	LocalAtOperation
	ResolveLocalOperation
	DateOfOperation
	TimeOfOperation
	CombineDateTimeOperation
	AddDateDaysOperation
	DateDifferenceOperation
	AddDateIntervalOperation
	SubtractDateIntervalOperation
	AddLocalIntervalOperation
	SubtractLocalIntervalOperation
	AddInstantIntervalOperation
	SubtractInstantIntervalOperation
	AddClockElapsedOperation
	SubtractClockElapsedOperation
	TransactionTimeOperation
	AddIntervalsOperation
	SubtractIntervalsOperation
	NegateIntervalOperation
	LocalDifferenceOperation
	InstantDifferenceOperation
	ClockDifferenceOperation
	IntervalMonthsOperation
	IntervalDaysOperation
	IntervalElapsedOperation
	JsonContainsOperation
	JsonContainedByOperation
	JsonKindOperation
	JsonPropertyOperation
	JsonIndexOperation
	JsonScalarOperation
	JsonUnquoteOperation
	JsonArrayLengthOperation
	SphericalDistanceOperation
)

// Operation names and arities are private, closed compiler metadata. Public
// builders never accept SQL function/operator/type names.
type OperationSpec struct {
	Name     string
	Min, Max int
	Infix    bool
}

func (op ScalarOperation) Spec() (OperationSpec, bool) {
	switch op {
	case SphericalDistanceOperation:
		return OperationSpec{"", 4, 4, false}, true
	case AddOperation:
		return OperationSpec{"+", 2, 2, true}, true
	case SubtractOperation:
		return OperationSpec{"-", 2, 2, true}, true
	case MultiplyOperation:
		return OperationSpec{"*", 2, 2, true}, true
	case DivideOperation:
		return OperationSpec{"/", 2, 2, true}, true
	case RemainderOperation:
		return OperationSpec{"%", 2, 2, true}, true
	case NegateOperation:
		return OperationSpec{"-", 1, 1, false}, true
	case AbsOperation:
		return OperationSpec{"ABS", 1, 1, false}, true
	case CastOperation:
		return OperationSpec{"", 1, 1, false}, true
	case LowerOperation:
		return OperationSpec{"LOWER", 1, 1, false}, true
	case UpperOperation:
		return OperationSpec{"UPPER", 1, 1, false}, true
	case TrimOperation:
		return OperationSpec{"BTRIM", 1, 2, false}, true
	case TrimLeftOperation:
		return OperationSpec{"LTRIM", 1, 2, false}, true
	case TrimRightOperation:
		return OperationSpec{"RTRIM", 1, 2, false}, true
	case LengthOperation:
		return OperationSpec{"CHAR_LENGTH", 1, 1, false}, true
	case OctetLengthOperation:
		return OperationSpec{"OCTET_LENGTH", 1, 1, false}, true
	case ConcatOperation:
		return OperationSpec{"||", 2, 2, true}, true
	case ConcatWSOperation:
		return OperationSpec{"CONCAT_WS", 2, MaxExpressionNodes, false}, true
	case SubstringOperation:
		return OperationSpec{"SUBSTR", 2, 3, false}, true
	case ReplaceOperation:
		return OperationSpec{"REPLACE", 3, 3, false}, true
	default:
		if op >= JsonContainsOperation {
			return JsonOperationSpec(op)
		}
		return TemporalOperationSpec(op)
	}
}

type OperationArgument struct {
	Value ValueExpression
	Kind  codec.ParameterType
}

type OperationNode struct {
	Kind      ScalarOperation
	Arguments []OperationArgument
	Result    codec.ParameterType
	Err       error
}

func (OperationNode) ValueNode() {}

func NumericRepresentation(kind codec.ParameterType) bool {
	return kind == codec.TypeInteger || kind == codec.TypeDecimal || kind == codec.TypeFloat
}

func (n OperationNode) Validate() error {
	if n.Err != nil {
		return n.Err
	}
	spec, ok := n.Kind.Spec()
	if !ok || len(n.Arguments) < spec.Min || len(n.Arguments) > spec.Max {
		return fault.New(fault.Invalid, "invalid scalar operation arguments")
	}
	if _, err := ParameterSQLType(n.Result); err != nil {
		return err
	}
	for _, a := range n.Arguments {
		if a.Value == nil {
			return fault.New(fault.Invalid, "scalar operation requires a declared value")
		}
		if _, err := ParameterSQLType(a.Kind); err != nil {
			return err
		}
	}
	if n.Kind == SphericalDistanceOperation {
		if n.Result != codec.TypeFloat {
			return fault.New(fault.Invalid, "distance requires a floating result")
		}
		for _, a := range n.Arguments {
			if a.Kind != codec.TypeFloat {
				return fault.New(fault.Invalid, "distance requires floating coordinates")
			}
		}
		return nil
	}
	if n.Kind >= JsonContainsOperation {
		return n.ValidateJSON()
	}
	if n.Kind >= ExtractYearOperation {
		return n.ValidateTemporal()
	}
	for i, a := range n.Arguments {
		switch {
		case n.Kind <= AbsOperation:
			if !NumericRepresentation(n.Result) || a.Kind != n.Result || (n.Kind == RemainderOperation && n.Result == codec.TypeFloat) {
				return fault.New(fault.Invalid, "arithmetic requires compatible numeric representations")
			}
		case n.Kind == CastOperation:
			if !NumericRepresentation(a.Kind) || (n.Result != codec.TypeDecimal && n.Result != codec.TypeFloat) {
				return fault.New(fault.Invalid, "invalid numeric conversion")
			}
		default:
			want := codec.TypeText
			if n.Kind == SubstringOperation && i > 0 {
				want = codec.TypeInteger
			}
			if a.Kind != want {
				return fault.New(fault.Invalid, "text operation has an incompatible representation")
			}
		}
	}
	if n.Kind >= LowerOperation {
		want := codec.TypeText
		if n.Kind == LengthOperation || n.Kind == OctetLengthOperation {
			want = codec.TypeInteger
		}
		if n.Result != want {
			return fault.New(fault.Invalid, "text operation has an incompatible result")
		}
	}
	return nil
}

// operationArguments compiles each argument with its explicit SQL type.
func (c *Compiler) OperationArguments(n OperationNode, grouped map[FieldRef]bool, grouping bool) ([]string, error) {
	args := make([]string, len(n.Arguments))
	argumentBytes := 0
	for i, a := range n.Arguments {
		text, err := c.SelectedExpression(a.Value, grouped, grouping)
		if err != nil {
			return nil, err
		}
		typeName, _ := ParameterSQLType(a.Kind)
		// SUBSTR uses SQL integer positions. Dynamic out-of-range positions fail
		// at the database; Go literal positions are int32 and are always bound.
		if (n.Kind == SubstringOperation && i > 0) || (n.Kind == JsonIndexOperation && i == 1) {
			typeName = "integer"
		}
		args[i] = "CAST(" + text + " AS " + typeName + ")"
		argumentBytes += len(args[i])
		if argumentBytes > MaxScalarSQLBytes {
			return nil, fault.New(fault.Invalid, "scalar SQL arguments exceed their resource bound")
		}
	}
	return args, nil
}

// jsonTextScalarSQL extracts a scalar directly below a property or element
// with ->>, which returns the same text as (document -> key) #>> '{}' (JSON
// null and missing paths are SQL NULL), so (document ->> 'key') expression
// indexes match typed predicates on declared properties.
func (c *Compiler) JsonTextScalarSQL(n OperationNode, grouped map[FieldRef]bool, grouping bool) (string, bool, error) {
	if n.Kind != JsonScalarOperation || len(n.Arguments) != 1 {
		return "", false, nil
	}
	inner, ok := n.Arguments[0].Value.(OperationNode)
	if !ok || (inner.Kind != JsonPropertyOperation && inner.Kind != JsonIndexOperation) {
		return "", false, nil
	}
	if err := inner.Validate(); err != nil {
		return "", true, err
	}
	args, err := c.OperationArguments(inner, grouped, grouping)
	if err != nil {
		return "", true, err
	}
	target, _ := ParameterSQLType(n.Result)
	return "CAST((" + args[0] + " ->> " + args[1] + ") AS " + target + ")", true, nil
}

func (c *Compiler) OperationSQL(n OperationNode, grouped map[FieldRef]bool, grouping bool) (sql string, err error) {
	defer func() {
		if err == nil {
			c.ScalarSQLBytes += len(c.render(sql, false))
			if c.ScalarSQLBytes > MaxScalarSQLBytes {
				sql, err = "", fault.New(fault.Invalid, "scalar SQL expansion exceeds its resource bound")
			}
		}
	}()
	if err := n.Validate(); err != nil {
		return "", err
	}
	if sql, collapsed, err := c.JsonTextScalarSQL(n, grouped, grouping); collapsed || err != nil {
		return sql, err
	}
	spec, _ := n.Kind.Spec()
	args, err := c.OperationArguments(n, grouped, grouping)
	if err != nil {
		return "", err
	}
	if n.Kind == SphericalDistanceOperation {
		return sphericalDistanceSQL(args), nil
	}
	if n.Kind >= JsonContainsOperation {
		return JsonOperationSQL(n, args)
	}
	if n.Kind >= ExtractYearOperation {
		return TemporalOperationSQL(n, args)
	}
	switch {
	case n.Kind == ConcatWSOperation:
		// A typed variadic array avoids PostgreSQL's ordinary function-argument
		// limit while retaining NULL elements for CONCAT_WS to skip.
		return "CONCAT_WS(" + args[0] + ", VARIADIC ARRAY[" + strings.Join(args[1:], ", ") + "])", nil
	case n.Kind == CastOperation:
		target, _ := ParameterSQLType(n.Result)
		return "CAST(" + args[0] + " AS " + target + ")", nil
	case n.Kind == NegateOperation:
		return "(-" + args[0] + ")", nil
	case spec.Infix:
		return "(" + args[0] + " " + spec.Name + " " + args[1] + ")", nil
	default:
		result := spec.Name + "(" + strings.Join(args, ", ") + ")"
		// String lengths are SQL integer; the public contract is int64.
		if n.Result == codec.TypeInteger {
			result = "CAST(" + result + " AS bigint)"
		}
		return result, nil
	}
}
