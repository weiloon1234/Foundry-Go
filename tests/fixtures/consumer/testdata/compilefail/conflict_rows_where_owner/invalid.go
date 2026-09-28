package invalid

import (
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/database/query"
)

var fields = models.WriteRecordFieldsAt(models.QueryWriteRecords().ConflictRows().Proposed())
var _ = query.OnConflict(models.UserFields().Email).Update(models.UserFields().Age).WhereRows(fields.Enabled.Eq(true))
