package mailing

import (
	"context"

	"github.com/weiloon1234/Foundry-Go/email"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
)

// doc-example:begin attach-request-upload
type UploadMailService struct {
	base email.Message
}

func (s *UploadMailService) Build(ctx context.Context, upload foundryhttp.UploadedFile) (email.Message, error) {
	message, err := s.base.AttachUpload(ctx, upload)
	if err != nil {
		return email.Message{}, err
	}
	return message, nil
}

// doc-example:end attach-request-upload

func NewUploadMailService(base email.Message) *UploadMailService {
	return &UploadMailService{base: base}
}
