package articles

import (
	"errors"

	"github.com/weiloon1234/Foundry-Go/email"
)

// doc-example:begin attach-stored-files
func ArticleImagesMessage(from, to email.Address, article Article) (email.Message, error) {
	message, err := email.NewMessage(from, "Article images", to).Text("Attached").AttachStored(article.Logo)
	if err != nil {
		return email.Message{}, err
	}
	for _, file := range article.Galleries.All() {
		message, err = message.AttachStored(file)
		if err != nil {
			return email.Message{}, err
		}
	}
	return message, nil
}

// doc-example:end attach-stored-files

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
