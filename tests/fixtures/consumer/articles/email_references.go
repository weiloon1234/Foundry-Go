package articles

import (
	"errors"

	"github.com/weiloon1234/Foundry-Go/email"
)

// doc-example:begin durable-email-references
func EmailReferences(article Article) ([]email.Attachment, error) {
	logo, err := article.Logo.EmailAttachment()
	if err != nil {
		return nil, err
	}
	gallery, loaded := article.Galleries.Get()
	if !loaded {
		return nil, errors.New("article gallery was not loaded")
	}
	files := []email.Attachment{logo}
	for _, file := range gallery {
		reference, err := file.EmailAttachment()
		if err != nil {
			return nil, err
		}
		files = append(files, reference)
	}
	return files, nil
}

// doc-example:end durable-email-references
