package invalid

import (
	"context"
	"github.com/weiloon1234/Foundry-Go/email"
	"strings"
)

func invalid(message email.Message) {
	_, _ = message.AttachUpload(context.Background(), strings.NewReader("not a captured upload"))
}
