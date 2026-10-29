package orm

type Column[V any] struct{}

func (c Column[V]) Eq(v V) int       { return 0 }
func (c Column[V]) Ne(v V) int       { return 0 }
func (c Column[V]) In(v ...V) int    { return 0 }
func (c Column[V]) NotIn(v ...V) int { return 0 }
func (c Column[V]) Asc() int         { return 0 }
func (c Column[V]) Desc() int        { return 0 }
func (c Column[V]) Set(v V) int      { return 0 }

type keyColumn[V any] struct{}
type TestKeyColumn[V any] = keyColumn[V]

func (c keyColumn[V]) Eq(v V) int       { return 0 }
func (c keyColumn[V]) Ne(v V) int       { return 0 }
func (c keyColumn[V]) In(v ...V) int    { return 0 }
func (c keyColumn[V]) NotIn(v ...V) int { return 0 }
func (c keyColumn[V]) Asc() int         { return 0 }
func (c keyColumn[V]) Desc() int        { return 0 }
func (c keyColumn[V]) Set(v V) int      { return 0 }

type numberColumn[V any] struct{}
type TestNumberColumn[V any] = numberColumn[V]

func (c numberColumn[V]) Eq(v V) int                         { return 0 }
func (c numberColumn[V]) Ne(v V) int                         { return 0 }
func (c numberColumn[V]) In(v ...V) int                      { return 0 }
func (c numberColumn[V]) NotIn(v ...V) int                   { return 0 }
func (c numberColumn[V]) Asc() int                           { return 0 }
func (c numberColumn[V]) Desc() int                          { return 0 }
func (c numberColumn[V]) Set(v V) int                        { return 0 }
func (c numberColumn[V]) Gt(v V) int                         { return 0 }
func (c numberColumn[V]) Gte(v V) int                        { return 0 }
func (c numberColumn[V]) Lt(v V) int                         { return 0 }
func (c numberColumn[V]) Lte(v V) int                        { return 0 }
func (c numberColumn[V]) Between(a, b V) int                 { return 0 }
func (c numberColumn[V]) Increment(amount V, bound ...V) int { return 0 }
func (c numberColumn[V]) Decrement(amount V, bound ...V) int { return 0 }

type textColumn[V any] struct{}
type TestTextColumn[V any] = textColumn[V]

func (c textColumn[V]) Eq(v V) int          { return 0 }
func (c textColumn[V]) Ne(v V) int          { return 0 }
func (c textColumn[V]) In(v ...V) int       { return 0 }
func (c textColumn[V]) NotIn(v ...V) int    { return 0 }
func (c textColumn[V]) Asc() int            { return 0 }
func (c textColumn[V]) Desc() int           { return 0 }
func (c textColumn[V]) Set(v V) int         { return 0 }
func (c textColumn[V]) Gt(v V) int          { return 0 }
func (c textColumn[V]) Gte(v V) int         { return 0 }
func (c textColumn[V]) Lt(v V) int          { return 0 }
func (c textColumn[V]) Lte(v V) int         { return 0 }
func (c textColumn[V]) Between(a, b V) int  { return 0 }
func (c textColumn[V]) Contains(v V) int    { return 0 }
func (c textColumn[V]) IContains(v V) int   { return 0 }
func (c textColumn[V]) StartsWith(v V) int  { return 0 }
func (c textColumn[V]) EndsWith(v V) int    { return 0 }
func (c textColumn[V]) Like(v V) int        { return 0 }
func (c textColumn[V]) ILike(v V) int       { return 0 }
func (c textColumn[V]) IStartsWith(v V) int { return 0 }
func (c textColumn[V]) IEndsWith(v V) int   { return 0 }

type timeColumn[V any] struct{}
type TestTimeColumn[V any] = timeColumn[V]

func (c timeColumn[V]) Eq(v V) int         { return 0 }
func (c timeColumn[V]) Ne(v V) int         { return 0 }
func (c timeColumn[V]) In(v ...V) int      { return 0 }
func (c timeColumn[V]) NotIn(v ...V) int   { return 0 }
func (c timeColumn[V]) Asc() int           { return 0 }
func (c timeColumn[V]) Desc() int          { return 0 }
func (c timeColumn[V]) Set(v V) int        { return 0 }
func (c timeColumn[V]) Gt(v V) int         { return 0 }
func (c timeColumn[V]) Gte(v V) int        { return 0 }
func (c timeColumn[V]) Lt(v V) int         { return 0 }
func (c timeColumn[V]) Lte(v V) int        { return 0 }
func (c timeColumn[V]) Between(a, b V) int { return 0 }

type Enum[V any] struct{}

func (c Enum[V]) Eq(v V) int       { return 0 }
func (c Enum[V]) Ne(v V) int       { return 0 }
func (c Enum[V]) In(v ...V) int    { return 0 }
func (c Enum[V]) NotIn(v ...V) int { return 0 }
func (c Enum[V]) Asc() int         { return 0 }
func (c Enum[V]) Desc() int        { return 0 }
func (c Enum[V]) Set(v V) int      { return 0 }

type NullableColumn[V any] struct{}

func (c NullableColumn[V]) Eq(v V) int       { return 0 }
func (c NullableColumn[V]) Ne(v V) int       { return 0 }
func (c NullableColumn[V]) In(v ...V) int    { return 0 }
func (c NullableColumn[V]) NotIn(v ...V) int { return 0 }
func (c NullableColumn[V]) Asc() int         { return 0 }
func (c NullableColumn[V]) Desc() int        { return 0 }
func (c NullableColumn[V]) Set(v V) int      { return 0 }
func (c NullableColumn[V]) IsNull() int      { return 0 }
func (c NullableColumn[V]) IsNotNull() int   { return 0 }
func (c NullableColumn[V]) SetNull() int     { return 0 }

type NullableNumber[V any] struct{}

func (c NullableNumber[V]) Eq(v V) int                         { return 0 }
func (c NullableNumber[V]) Ne(v V) int                         { return 0 }
func (c NullableNumber[V]) In(v ...V) int                      { return 0 }
func (c NullableNumber[V]) NotIn(v ...V) int                   { return 0 }
func (c NullableNumber[V]) Asc() int                           { return 0 }
func (c NullableNumber[V]) Desc() int                          { return 0 }
func (c NullableNumber[V]) Set(v V) int                        { return 0 }
func (c NullableNumber[V]) Gt(v V) int                         { return 0 }
func (c NullableNumber[V]) Gte(v V) int                        { return 0 }
func (c NullableNumber[V]) Lt(v V) int                         { return 0 }
func (c NullableNumber[V]) Lte(v V) int                        { return 0 }
func (c NullableNumber[V]) Between(a, b V) int                 { return 0 }
func (c NullableNumber[V]) IsNull() int                        { return 0 }
func (c NullableNumber[V]) IsNotNull() int                     { return 0 }
func (c NullableNumber[V]) SetNull() int                       { return 0 }
func (c NullableNumber[V]) Increment(amount V, bound ...V) int { return 0 }
func (c NullableNumber[V]) Decrement(amount V, bound ...V) int { return 0 }

type NullableText[V any] struct{}

func (c NullableText[V]) Eq(v V) int          { return 0 }
func (c NullableText[V]) Ne(v V) int          { return 0 }
func (c NullableText[V]) In(v ...V) int       { return 0 }
func (c NullableText[V]) NotIn(v ...V) int    { return 0 }
func (c NullableText[V]) Asc() int            { return 0 }
func (c NullableText[V]) Desc() int           { return 0 }
func (c NullableText[V]) Set(v V) int         { return 0 }
func (c NullableText[V]) Gt(v V) int          { return 0 }
func (c NullableText[V]) Gte(v V) int         { return 0 }
func (c NullableText[V]) Lt(v V) int          { return 0 }
func (c NullableText[V]) Lte(v V) int         { return 0 }
func (c NullableText[V]) Between(a, b V) int  { return 0 }
func (c NullableText[V]) Contains(v V) int    { return 0 }
func (c NullableText[V]) IContains(v V) int   { return 0 }
func (c NullableText[V]) StartsWith(v V) int  { return 0 }
func (c NullableText[V]) EndsWith(v V) int    { return 0 }
func (c NullableText[V]) Like(v V) int        { return 0 }
func (c NullableText[V]) ILike(v V) int       { return 0 }
func (c NullableText[V]) IStartsWith(v V) int { return 0 }
func (c NullableText[V]) IEndsWith(v V) int   { return 0 }
func (c NullableText[V]) IsNull() int         { return 0 }
func (c NullableText[V]) IsNotNull() int      { return 0 }
func (c NullableText[V]) SetNull() int        { return 0 }

type nullableTimeColumn[V any] struct{}
type TestNullableTimeColumn[V any] = nullableTimeColumn[V]

func (c nullableTimeColumn[V]) Eq(v V) int         { return 0 }
func (c nullableTimeColumn[V]) Ne(v V) int         { return 0 }
func (c nullableTimeColumn[V]) In(v ...V) int      { return 0 }
func (c nullableTimeColumn[V]) NotIn(v ...V) int   { return 0 }
func (c nullableTimeColumn[V]) Asc() int           { return 0 }
func (c nullableTimeColumn[V]) Desc() int          { return 0 }
func (c nullableTimeColumn[V]) Set(v V) int        { return 0 }
func (c nullableTimeColumn[V]) Gt(v V) int         { return 0 }
func (c nullableTimeColumn[V]) Gte(v V) int        { return 0 }
func (c nullableTimeColumn[V]) Lt(v V) int         { return 0 }
func (c nullableTimeColumn[V]) Lte(v V) int        { return 0 }
func (c nullableTimeColumn[V]) Between(a, b V) int { return 0 }
func (c nullableTimeColumn[V]) IsNull() int        { return 0 }
func (c nullableTimeColumn[V]) IsNotNull() int     { return 0 }
func (c nullableTimeColumn[V]) SetNull() int       { return 0 }

type NullableEnum[V any] struct{}

func (c NullableEnum[V]) Eq(v V) int       { return 0 }
func (c NullableEnum[V]) Ne(v V) int       { return 0 }
func (c NullableEnum[V]) In(v ...V) int    { return 0 }
func (c NullableEnum[V]) NotIn(v ...V) int { return 0 }
func (c NullableEnum[V]) Asc() int         { return 0 }
func (c NullableEnum[V]) Desc() int        { return 0 }
func (c NullableEnum[V]) Set(v V) int      { return 0 }
func (c NullableEnum[V]) IsNull() int      { return 0 }
func (c NullableEnum[V]) IsNotNull() int   { return 0 }
func (c NullableEnum[V]) SetNull() int     { return 0 }

type InputColumn[V any] struct{}

func (c InputColumn[V]) Eq(v V) int       { return 0 }
func (c InputColumn[V]) Ne(v V) int       { return 0 }
func (c InputColumn[V]) In(v ...V) int    { return 0 }
func (c InputColumn[V]) NotIn(v ...V) int { return 0 }
func (c InputColumn[V]) Asc() int         { return 0 }
func (c InputColumn[V]) Desc() int        { return 0 }
func (c InputColumn[V]) Set(v V) int      { return 0 }

type NullableInputColumn[V any] struct{}

func (c NullableInputColumn[V]) Eq(v V) int       { return 0 }
func (c NullableInputColumn[V]) Ne(v V) int       { return 0 }
func (c NullableInputColumn[V]) In(v ...V) int    { return 0 }
func (c NullableInputColumn[V]) NotIn(v ...V) int { return 0 }
func (c NullableInputColumn[V]) Asc() int         { return 0 }
func (c NullableInputColumn[V]) Desc() int        { return 0 }
func (c NullableInputColumn[V]) Set(v V) int      { return 0 }
func (c NullableInputColumn[V]) IsNull() int      { return 0 }
func (c NullableInputColumn[V]) IsNotNull() int   { return 0 }
func (c NullableInputColumn[V]) SetNull() int     { return 0 }

type InputText[V any] struct{}

func (c InputText[V]) Eq(v V) int          { return 0 }
func (c InputText[V]) Ne(v V) int          { return 0 }
func (c InputText[V]) In(v ...V) int       { return 0 }
func (c InputText[V]) NotIn(v ...V) int    { return 0 }
func (c InputText[V]) Asc() int            { return 0 }
func (c InputText[V]) Desc() int           { return 0 }
func (c InputText[V]) Set(v V) int         { return 0 }
func (c InputText[V]) Gt(v V) int          { return 0 }
func (c InputText[V]) Gte(v V) int         { return 0 }
func (c InputText[V]) Lt(v V) int          { return 0 }
func (c InputText[V]) Lte(v V) int         { return 0 }
func (c InputText[V]) Between(a, b V) int  { return 0 }
func (c InputText[V]) Contains(v V) int    { return 0 }
func (c InputText[V]) IContains(v V) int   { return 0 }
func (c InputText[V]) StartsWith(v V) int  { return 0 }
func (c InputText[V]) EndsWith(v V) int    { return 0 }
func (c InputText[V]) Like(v V) int        { return 0 }
func (c InputText[V]) ILike(v V) int       { return 0 }
func (c InputText[V]) IStartsWith(v V) int { return 0 }
func (c InputText[V]) IEndsWith(v V) int   { return 0 }

type NullableInputText[V any] struct{}

func (c NullableInputText[V]) Eq(v V) int          { return 0 }
func (c NullableInputText[V]) Ne(v V) int          { return 0 }
func (c NullableInputText[V]) In(v ...V) int       { return 0 }
func (c NullableInputText[V]) NotIn(v ...V) int    { return 0 }
func (c NullableInputText[V]) Asc() int            { return 0 }
func (c NullableInputText[V]) Desc() int           { return 0 }
func (c NullableInputText[V]) Set(v V) int         { return 0 }
func (c NullableInputText[V]) Gt(v V) int          { return 0 }
func (c NullableInputText[V]) Gte(v V) int         { return 0 }
func (c NullableInputText[V]) Lt(v V) int          { return 0 }
func (c NullableInputText[V]) Lte(v V) int         { return 0 }
func (c NullableInputText[V]) Between(a, b V) int  { return 0 }
func (c NullableInputText[V]) Contains(v V) int    { return 0 }
func (c NullableInputText[V]) IContains(v V) int   { return 0 }
func (c NullableInputText[V]) StartsWith(v V) int  { return 0 }
func (c NullableInputText[V]) EndsWith(v V) int    { return 0 }
func (c NullableInputText[V]) Like(v V) int        { return 0 }
func (c NullableInputText[V]) ILike(v V) int       { return 0 }
func (c NullableInputText[V]) IStartsWith(v V) int { return 0 }
func (c NullableInputText[V]) IEndsWith(v V) int   { return 0 }
func (c NullableInputText[V]) IsNull() int         { return 0 }
func (c NullableInputText[V]) IsNotNull() int      { return 0 }
func (c NullableInputText[V]) SetNull() int        { return 0 }

type InputNumber[V any] struct{}

func (c InputNumber[V]) Eq(v V) int         { return 0 }
func (c InputNumber[V]) Ne(v V) int         { return 0 }
func (c InputNumber[V]) In(v ...V) int      { return 0 }
func (c InputNumber[V]) NotIn(v ...V) int   { return 0 }
func (c InputNumber[V]) Asc() int           { return 0 }
func (c InputNumber[V]) Desc() int          { return 0 }
func (c InputNumber[V]) Set(v V) int        { return 0 }
func (c InputNumber[V]) Gt(v V) int         { return 0 }
func (c InputNumber[V]) Gte(v V) int        { return 0 }
func (c InputNumber[V]) Lt(v V) int         { return 0 }
func (c InputNumber[V]) Lte(v V) int        { return 0 }
func (c InputNumber[V]) Between(a, b V) int { return 0 }

type NullableInputNumber[V any] struct{}

func (c NullableInputNumber[V]) Eq(v V) int         { return 0 }
func (c NullableInputNumber[V]) Ne(v V) int         { return 0 }
func (c NullableInputNumber[V]) In(v ...V) int      { return 0 }
func (c NullableInputNumber[V]) NotIn(v ...V) int   { return 0 }
func (c NullableInputNumber[V]) Asc() int           { return 0 }
func (c NullableInputNumber[V]) Desc() int          { return 0 }
func (c NullableInputNumber[V]) Set(v V) int        { return 0 }
func (c NullableInputNumber[V]) Gt(v V) int         { return 0 }
func (c NullableInputNumber[V]) Gte(v V) int        { return 0 }
func (c NullableInputNumber[V]) Lt(v V) int         { return 0 }
func (c NullableInputNumber[V]) Lte(v V) int        { return 0 }
func (c NullableInputNumber[V]) Between(a, b V) int { return 0 }
func (c NullableInputNumber[V]) IsNull() int        { return 0 }
func (c NullableInputNumber[V]) IsNotNull() int     { return 0 }
func (c NullableInputNumber[V]) SetNull() int       { return 0 }

type InputTime[V any] struct{}

func (c InputTime[V]) Eq(v V) int         { return 0 }
func (c InputTime[V]) Ne(v V) int         { return 0 }
func (c InputTime[V]) In(v ...V) int      { return 0 }
func (c InputTime[V]) NotIn(v ...V) int   { return 0 }
func (c InputTime[V]) Asc() int           { return 0 }
func (c InputTime[V]) Desc() int          { return 0 }
func (c InputTime[V]) Set(v V) int        { return 0 }
func (c InputTime[V]) Gt(v V) int         { return 0 }
func (c InputTime[V]) Gte(v V) int        { return 0 }
func (c InputTime[V]) Lt(v V) int         { return 0 }
func (c InputTime[V]) Lte(v V) int        { return 0 }
func (c InputTime[V]) Between(a, b V) int { return 0 }

type NullableInputTime[V any] struct{}

func (c NullableInputTime[V]) Eq(v V) int         { return 0 }
func (c NullableInputTime[V]) Ne(v V) int         { return 0 }
func (c NullableInputTime[V]) In(v ...V) int      { return 0 }
func (c NullableInputTime[V]) NotIn(v ...V) int   { return 0 }
func (c NullableInputTime[V]) Asc() int           { return 0 }
func (c NullableInputTime[V]) Desc() int          { return 0 }
func (c NullableInputTime[V]) Set(v V) int        { return 0 }
func (c NullableInputTime[V]) Gt(v V) int         { return 0 }
func (c NullableInputTime[V]) Gte(v V) int        { return 0 }
func (c NullableInputTime[V]) Lt(v V) int         { return 0 }
func (c NullableInputTime[V]) Lte(v V) int        { return 0 }
func (c NullableInputTime[V]) Between(a, b V) int { return 0 }
func (c NullableInputTime[V]) IsNull() int        { return 0 }
func (c NullableInputTime[V]) IsNotNull() int     { return 0 }
func (c NullableInputTime[V]) SetNull() int       { return 0 }

type InputEnum[V any] struct{}

func (c InputEnum[V]) Eq(v V) int       { return 0 }
func (c InputEnum[V]) Ne(v V) int       { return 0 }
func (c InputEnum[V]) In(v ...V) int    { return 0 }
func (c InputEnum[V]) NotIn(v ...V) int { return 0 }
func (c InputEnum[V]) Asc() int         { return 0 }
func (c InputEnum[V]) Desc() int        { return 0 }
func (c InputEnum[V]) Set(v V) int      { return 0 }

type NullableInputEnum[V any] struct{}

func (c NullableInputEnum[V]) Eq(v V) int       { return 0 }
func (c NullableInputEnum[V]) Ne(v V) int       { return 0 }
func (c NullableInputEnum[V]) In(v ...V) int    { return 0 }
func (c NullableInputEnum[V]) NotIn(v ...V) int { return 0 }
func (c NullableInputEnum[V]) Asc() int         { return 0 }
func (c NullableInputEnum[V]) Desc() int        { return 0 }
func (c NullableInputEnum[V]) Set(v V) int      { return 0 }
func (c NullableInputEnum[V]) IsNull() int      { return 0 }
func (c NullableInputEnum[V]) IsNotNull() int   { return 0 }
func (c NullableInputEnum[V]) SetNull() int     { return 0 }

type InputJSON[V any] struct{}

func (c InputJSON[V]) Eq(v V) int          { return 0 }
func (c InputJSON[V]) Ne(v V) int          { return 0 }
func (c InputJSON[V]) In(v ...V) int       { return 0 }
func (c InputJSON[V]) NotIn(v ...V) int    { return 0 }
func (c InputJSON[V]) Asc() int            { return 0 }
func (c InputJSON[V]) Desc() int           { return 0 }
func (c InputJSON[V]) Contains(v V) int    { return 0 }
func (c InputJSON[V]) ContainedBy(v V) int { return 0 }
func (c InputJSON[V]) IsJSONNull() int     { return 0 }
func (c InputJSON[V]) Set(v V) int         { return 0 }

type NullableInputJSON[V any] struct{}

func (c NullableInputJSON[V]) Eq(v V) int          { return 0 }
func (c NullableInputJSON[V]) Ne(v V) int          { return 0 }
func (c NullableInputJSON[V]) In(v ...V) int       { return 0 }
func (c NullableInputJSON[V]) NotIn(v ...V) int    { return 0 }
func (c NullableInputJSON[V]) Asc() int            { return 0 }
func (c NullableInputJSON[V]) Desc() int           { return 0 }
func (c NullableInputJSON[V]) Contains(v V) int    { return 0 }
func (c NullableInputJSON[V]) ContainedBy(v V) int { return 0 }
func (c NullableInputJSON[V]) IsJSONNull() int     { return 0 }
func (c NullableInputJSON[V]) IsNull() int         { return 0 }
func (c NullableInputJSON[V]) IsNotNull() int      { return 0 }
func (c NullableInputJSON[V]) Set(v V) int         { return 0 }
func (c NullableInputJSON[V]) SetNull() int        { return 0 }

type InputInterval[V any] struct{}

func (c InputInterval[V]) Eq(v V) int         { return 0 }
func (c InputInterval[V]) Ne(v V) int         { return 0 }
func (c InputInterval[V]) In(v ...V) int      { return 0 }
func (c InputInterval[V]) NotIn(v ...V) int   { return 0 }
func (c InputInterval[V]) Asc() int           { return 0 }
func (c InputInterval[V]) Desc() int          { return 0 }
func (c InputInterval[V]) Set(v V) int        { return 0 }
func (c InputInterval[V]) Gt(v V) int         { return 0 }
func (c InputInterval[V]) Gte(v V) int        { return 0 }
func (c InputInterval[V]) Lt(v V) int         { return 0 }
func (c InputInterval[V]) Lte(v V) int        { return 0 }
func (c InputInterval[V]) Between(a, b V) int { return 0 }

type NullableInputInterval[V any] struct{}

func (c NullableInputInterval[V]) Eq(v V) int         { return 0 }
func (c NullableInputInterval[V]) Ne(v V) int         { return 0 }
func (c NullableInputInterval[V]) In(v ...V) int      { return 0 }
func (c NullableInputInterval[V]) NotIn(v ...V) int   { return 0 }
func (c NullableInputInterval[V]) Asc() int           { return 0 }
func (c NullableInputInterval[V]) Desc() int          { return 0 }
func (c NullableInputInterval[V]) Set(v V) int        { return 0 }
func (c NullableInputInterval[V]) Gt(v V) int         { return 0 }
func (c NullableInputInterval[V]) Gte(v V) int        { return 0 }
func (c NullableInputInterval[V]) Lt(v V) int         { return 0 }
func (c NullableInputInterval[V]) Lte(v V) int        { return 0 }
func (c NullableInputInterval[V]) Between(a, b V) int { return 0 }
func (c NullableInputInterval[V]) IsNull() int        { return 0 }
func (c NullableInputInterval[V]) IsNotNull() int     { return 0 }
func (c NullableInputInterval[V]) SetNull() int       { return 0 }

type JSON[V any] struct{}

func (c JSON[V]) Eq(v V) int          { return 0 }
func (c JSON[V]) Ne(v V) int          { return 0 }
func (c JSON[V]) In(v ...V) int       { return 0 }
func (c JSON[V]) NotIn(v ...V) int    { return 0 }
func (c JSON[V]) Asc() int            { return 0 }
func (c JSON[V]) Desc() int           { return 0 }
func (c JSON[V]) Contains(v V) int    { return 0 }
func (c JSON[V]) ContainedBy(v V) int { return 0 }
func (c JSON[V]) IsJSONNull() int     { return 0 }
func (c JSON[V]) Set(v V) int         { return 0 }

type NullableJSON[V any] struct{}

func (c NullableJSON[V]) Eq(v V) int          { return 0 }
func (c NullableJSON[V]) Ne(v V) int          { return 0 }
func (c NullableJSON[V]) In(v ...V) int       { return 0 }
func (c NullableJSON[V]) NotIn(v ...V) int    { return 0 }
func (c NullableJSON[V]) Asc() int            { return 0 }
func (c NullableJSON[V]) Desc() int           { return 0 }
func (c NullableJSON[V]) Contains(v V) int    { return 0 }
func (c NullableJSON[V]) ContainedBy(v V) int { return 0 }
func (c NullableJSON[V]) IsJSONNull() int     { return 0 }
func (c NullableJSON[V]) IsNull() int         { return 0 }
func (c NullableJSON[V]) IsNotNull() int      { return 0 }
func (c NullableJSON[V]) Set(v V) int         { return 0 }
func (c NullableJSON[V]) SetNull() int        { return 0 }

type Interval[V any] struct{}

func (c Interval[V]) Eq(v V) int         { return 0 }
func (c Interval[V]) Ne(v V) int         { return 0 }
func (c Interval[V]) In(v ...V) int      { return 0 }
func (c Interval[V]) NotIn(v ...V) int   { return 0 }
func (c Interval[V]) Asc() int           { return 0 }
func (c Interval[V]) Desc() int          { return 0 }
func (c Interval[V]) Set(v V) int        { return 0 }
func (c Interval[V]) Gt(v V) int         { return 0 }
func (c Interval[V]) Gte(v V) int        { return 0 }
func (c Interval[V]) Lt(v V) int         { return 0 }
func (c Interval[V]) Lte(v V) int        { return 0 }
func (c Interval[V]) Between(a, b V) int { return 0 }

type NullableInterval[V any] struct{}

func (c NullableInterval[V]) Eq(v V) int         { return 0 }
func (c NullableInterval[V]) Ne(v V) int         { return 0 }
func (c NullableInterval[V]) In(v ...V) int      { return 0 }
func (c NullableInterval[V]) NotIn(v ...V) int   { return 0 }
func (c NullableInterval[V]) Asc() int           { return 0 }
func (c NullableInterval[V]) Desc() int          { return 0 }
func (c NullableInterval[V]) Set(v V) int        { return 0 }
func (c NullableInterval[V]) Gt(v V) int         { return 0 }
func (c NullableInterval[V]) Gte(v V) int        { return 0 }
func (c NullableInterval[V]) Lt(v V) int         { return 0 }
func (c NullableInterval[V]) Lte(v V) int        { return 0 }
func (c NullableInterval[V]) Between(a, b V) int { return 0 }
func (c NullableInterval[V]) IsNull() int        { return 0 }
func (c NullableInterval[V]) IsNotNull() int     { return 0 }
func (c NullableInterval[V]) SetNull() int       { return 0 }

type Encrypted[V any] struct{}

func (c Encrypted[V]) Set(v V) int { return 0 }

type NullableEncrypted[V any] struct{}

func (c NullableEncrypted[V]) Set(v V) int    { return 0 }
func (c NullableEncrypted[V]) SetNull() int   { return 0 }
func (c NullableEncrypted[V]) IsNull() int    { return 0 }
func (c NullableEncrypted[V]) IsNotNull() int { return 0 }

type numberKeyColumn[V any] struct{}
type TestNumberKeyColumn[V any] = numberKeyColumn[V]

func (c numberKeyColumn[V]) Eq(v V) int         { return 0 }
func (c numberKeyColumn[V]) Ne(v V) int         { return 0 }
func (c numberKeyColumn[V]) In(v ...V) int      { return 0 }
func (c numberKeyColumn[V]) NotIn(v ...V) int   { return 0 }
func (c numberKeyColumn[V]) Asc() int           { return 0 }
func (c numberKeyColumn[V]) Desc() int          { return 0 }
func (c numberKeyColumn[V]) Set(v V) int        { return 0 }
func (c numberKeyColumn[V]) Gt(v V) int         { return 0 }
func (c numberKeyColumn[V]) Gte(v V) int        { return 0 }
func (c numberKeyColumn[V]) Lt(v V) int         { return 0 }
func (c numberKeyColumn[V]) Lte(v V) int        { return 0 }
func (c numberKeyColumn[V]) Between(a, b V) int { return 0 }

type InputKey[V any] struct{}

func (c InputKey[V]) Eq(v V) int       { return 0 }
func (c InputKey[V]) Ne(v V) int       { return 0 }
func (c InputKey[V]) In(v ...V) int    { return 0 }
func (c InputKey[V]) NotIn(v ...V) int { return 0 }
func (c InputKey[V]) Asc() int         { return 0 }
func (c InputKey[V]) Desc() int        { return 0 }
func (c InputKey[V]) Set(v V) int      { return 0 }
