package codecs

import (
	"foundry.test/consumer/models"
)

func statusCodecExample() error {
	// doc-example:begin database-codecs-1
	statusCodec := models.StatusCodec()
	bound, err := statusCodec.Bind(models.StatusActive)
	if err != nil {
		return err
	}
	// Pass bound as a SQL parameter after checking err.
	var status models.Status
	scanner := statusCodec.Scan(&status)
	// Pass scanner as a scan destination for the matching column.
	// doc-example:end database-codecs-1
	_ = bound
	_ = scanner
	return err
}
