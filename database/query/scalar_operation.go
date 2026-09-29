package query

import (
	"slices"
	"strings"

	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/fault"
)

type scalarOperation uint8

const (
	addOperation scalarOperation = iota + 1
	subtractOperation
	multiplyOperation
	divideOperation
	remainderOperation
	negateOperation
	absOperation
	castOperation
	lowerOperation
	upperOperation
	trimOperation
	trimLeftOperation
	trimRightOperation
	lengthOperation
	octetLengthOperation
	concatOperation
	concatWSOperation
	substringOperation
	replaceOperation
	extractYearOperation
	extractMonthOperation
	extractDayOperation
	extractDayOfWeekOperation
	extractISODayOfWeekOperation
	extractDayOfYearOperation
	extractISOWeekOperation
	extractISOYearOperation
	extractQuarterOperation
	extractCenturyOperation
	extractDecadeOperation
	extractMillenniumOperation
	extractJulianDayOperation
	extractHourOperation
	extractMinuteOperation
	extractSecondOperation
	extractMillisecondsOperation
	extractMicrosecondsOperation
	extractUnixSecondsOperation
	extractUnixMillisecondsOperation
	fromUnixMillisOperation
	truncateDateOperation
	truncateLocalOperation
	truncateInstantOperation
	localAtOperation
	resolveLocalOperation
	dateOfOperation
	timeOfOperation
	combineDateTimeOperation
	addDateDaysOperation
	dateDifferenceOperation
	addDateIntervalOperation
	subtractDateIntervalOperation
	addLocalIntervalOperation
	subtractLocalIntervalOperation
	addInstantIntervalOperation
	subtractInstantIntervalOperation
	addClockElapsedOperation
	subtractClockElapsedOperation
	transactionTimeOperation
	addIntervalsOperation
	subtractIntervalsOperation
	negateIntervalOperation
	localDifferenceOperation
	instantDifferenceOperation
	clockDifferenceOperation
	intervalMonthsOperation
	intervalDaysOperation
	intervalElapsedOperation
	jsonContainsOperation
	jsonContainedByOperation
	jsonKindOperation
	jsonPropertyOperation
	jsonIndexOperation
	jsonScalarOperation
	jsonUnquoteOperation
	jsonArrayLengthOperation
)

// Operation names and arities are private, closed compiler metadata. Public
// builders never accept SQL function/operator/type names.
type operationSpec struct {
	name     string
	min, max int
	infix    bool
}

func (op scalarOperation) spec() (operationSpec, bool) {
	switch op {
	case addOperation:
		return operationSpec{"+", 2, 2, true}, true
	case subtractOperation:
		return operationSpec{"-", 2, 2, true}, true
	case multiplyOperation:
		return operationSpec{"*", 2, 2, true}, true
	case divideOperation:
		return operationSpec{"/", 2, 2, true}, true
	case remainderOperation:
		return operationSpec{"%", 2, 2, true}, true
	case negateOperation:
		return operationSpec{"-", 1, 1, false}, true
	case absOperation:
		return operationSpec{"ABS", 1, 1, false}, true
	case castOperation:
		return operationSpec{"", 1, 1, false}, true
	case lowerOperation:
		return operationSpec{"LOWER", 1, 1, false}, true
	case upperOperation:
		return operationSpec{"UPPER", 1, 1, false}, true
	case trimOperation:
		return operationSpec{"BTRIM", 1, 2, false}, true
	case trimLeftOperation:
		return operationSpec{"LTRIM", 1, 2, false}, true
	case trimRightOperation:
		return operationSpec{"RTRIM", 1, 2, false}, true
	case lengthOperation:
		return operationSpec{"CHAR_LENGTH", 1, 1, false}, true
	case octetLengthOperation:
		return operationSpec{"OCTET_LENGTH", 1, 1, false}, true
	case concatOperation:
		return operationSpec{"||", 2, 2, true}, true
	case concatWSOperation:
		return operationSpec{"CONCAT_WS", 2, MaxExpressionNodes, false}, true
	case substringOperation:
		return operationSpec{"SUBSTR", 2, 3, false}, true
	case replaceOperation:
		return operationSpec{"REPLACE", 3, 3, false}, true
	default:
		if op >= jsonContainsOperation {
			return jsonOperationSpec(op)
		}
		return temporalOperationSpec(op)
	}
}

type operationArgument struct {
	value valueExpression
	kind  codec.ParameterType
}
type operationNode struct {
	kind      scalarOperation
	arguments []operationArgument
	result    codec.ParameterType
	err       error
}

func (operationNode) valueNode() {}
func operationArg[S, V any](e Expression[S, V]) operationArgument {
	return operationArgument{e.node, e.codec.ParameterType()}
}
func operationValue[S, V any](op scalarOperation, result codec.Codec[V], arguments ...operationArgument) Expression[S, V] {
	return Expression[S, V]{node: operationNode{kind: op, arguments: slices.Clone(arguments), result: result.ParameterType()}, codec: result}
}
func numericRepresentation(kind codec.ParameterType) bool {
	return kind == codec.TypeInteger || kind == codec.TypeDecimal || kind == codec.TypeFloat
}
func (n operationNode) validate() error {
	if n.err != nil {
		return n.err
	}
	spec, ok := n.kind.spec()
	if !ok || len(n.arguments) < spec.min || len(n.arguments) > spec.max {
		return fault.New(fault.Invalid, "invalid scalar operation arguments")
	}
	if _, err := parameterSQLType(n.result); err != nil {
		return err
	}
	for _, a := range n.arguments {
		if a.value == nil {
			return fault.New(fault.Invalid, "scalar operation requires a declared value")
		}
		if _, err := parameterSQLType(a.kind); err != nil {
			return err
		}
	}
	if n.kind >= jsonContainsOperation {
		return n.validateJSON()
	}
	if n.kind >= extractYearOperation {
		return n.validateTemporal()
	}
	for i, a := range n.arguments {
		switch {
		case n.kind <= absOperation:
			if !numericRepresentation(n.result) || a.kind != n.result || (n.kind == remainderOperation && n.result == codec.TypeFloat) {
				return fault.New(fault.Invalid, "arithmetic requires compatible numeric representations")
			}
		case n.kind == castOperation:
			if !numericRepresentation(a.kind) || (n.result != codec.TypeDecimal && n.result != codec.TypeFloat) {
				return fault.New(fault.Invalid, "invalid numeric conversion")
			}
		default:
			want := codec.TypeText
			if n.kind == substringOperation && i > 0 {
				want = codec.TypeInteger
			}
			if a.kind != want {
				return fault.New(fault.Invalid, "text operation has an incompatible representation")
			}
		}
	}
	if n.kind >= lowerOperation {
		want := codec.TypeText
		if n.kind == lengthOperation || n.kind == octetLengthOperation {
			want = codec.TypeInteger
		}
		if n.result != want {
			return fault.New(fault.Invalid, "text operation has an incompatible result")
		}
	}
	return nil
}

// operationArguments compiles each argument with its explicit SQL type.
func (c *compiler) operationArguments(n operationNode, grouped map[fieldRef]bool, grouping bool) ([]string, error) {
	args := make([]string, len(n.arguments))
	argumentBytes := 0
	for i, a := range n.arguments {
		text, err := c.selectedExpression(a.value, grouped, grouping)
		if err != nil {
			return nil, err
		}
		typeName, _ := parameterSQLType(a.kind)
		// SUBSTR uses SQL integer positions. Dynamic out-of-range positions fail
		// at the database; Go literal positions are int32 and are always bound.
		if (n.kind == substringOperation && i > 0) || (n.kind == jsonIndexOperation && i == 1) {
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
func (c *compiler) jsonTextScalarSQL(n operationNode, grouped map[fieldRef]bool, grouping bool) (string, bool, error) {
	if n.kind != jsonScalarOperation || len(n.arguments) != 1 {
		return "", false, nil
	}
	inner, ok := n.arguments[0].value.(operationNode)
	if !ok || (inner.kind != jsonPropertyOperation && inner.kind != jsonIndexOperation) {
		return "", false, nil
	}
	if err := inner.validate(); err != nil {
		return "", true, err
	}
	args, err := c.operationArguments(inner, grouped, grouping)
	if err != nil {
		return "", true, err
	}
	target, _ := parameterSQLType(n.result)
	return "CAST((" + args[0] + " ->> " + args[1] + ") AS " + target + ")", true, nil
}

func (c *compiler) operationSQL(n operationNode, grouped map[fieldRef]bool, grouping bool) (sql string, err error) {
	defer func() {
		if err == nil {
			c.scalarSQLBytes += len(sql)
			if c.scalarSQLBytes > MaxScalarSQLBytes {
				sql, err = "", fault.New(fault.Invalid, "scalar SQL expansion exceeds its resource bound")
			}
		}
	}()
	if err := n.validate(); err != nil {
		return "", err
	}
	if sql, collapsed, err := c.jsonTextScalarSQL(n, grouped, grouping); collapsed || err != nil {
		return sql, err
	}
	spec, _ := n.kind.spec()
	args, err := c.operationArguments(n, grouped, grouping)
	if err != nil {
		return "", err
	}
	if n.kind >= jsonContainsOperation {
		return jsonOperationSQL(n, args)
	}
	if n.kind >= extractYearOperation {
		return temporalOperationSQL(n, args)
	}
	switch {
	case n.kind == concatWSOperation:
		// A typed variadic array avoids PostgreSQL's ordinary function-argument
		// limit while retaining NULL elements for CONCAT_WS to skip.
		return "CONCAT_WS(" + args[0] + ", VARIADIC ARRAY[" + strings.Join(args[1:], ", ") + "])", nil
	case n.kind == castOperation:
		target, _ := parameterSQLType(n.result)
		return "CAST(" + args[0] + " AS " + target + ")", nil
	case n.kind == negateOperation:
		return "(-" + args[0] + ")", nil
	case spec.infix:
		return "(" + args[0] + " " + spec.name + " " + args[1] + ")", nil
	default:
		result := spec.name + "(" + strings.Join(args, ", ") + ")"
		// String lengths are SQL integer; the public contract is int64.
		if n.result == codec.TypeInteger {
			result = "CAST(" + result + " AS bigint)"
		}
		return result, nil
	}
}
