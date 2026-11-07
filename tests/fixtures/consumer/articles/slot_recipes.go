package articles

import (
	"context"
	"errors"

	"github.com/weiloon1234/Foundry-Go/application"
	"github.com/weiloon1234/Foundry-Go/i18n"
	"github.com/weiloon1234/Foundry-Go/value"
)

// doc-example:begin configure-slots
func ConfigureSlots(settings application.Settings) *application.Builder {
	return application.New(settings, application.WithTables(Tables()...)).Models(ORMExtensionDeclaration())
}

func ServiceFrom(services application.Services) (*Service, error) {
	runtime, err := services.ModelExtensions()
	if err != nil {
		return nil, err
	}
	return NewService(runtime.Store, runtime), nil
}

// doc-example:end configure-slots

// doc-example:begin loaded-slot-values
func EditorValues(article Article) (map[i18n.LocaleID]string, value.Optional[SEO], error) {
	title, loaded := article.Title.Values()
	if !loaded {
		return nil, value.Optional[SEO]{}, errors.New("article title was not loaded")
	}
	seo, loaded := article.SEO.Get()
	if !loaded {
		return nil, value.Optional[SEO]{}, errors.New("article metadata was not loaded")
	}
	return title.Entries(), seo, nil
}

// doc-example:end loaded-slot-values

// doc-example:begin resolve-slot-text
func DisplayTitle(ctx context.Context, article Article) (value.Optional[string], error) {
	resolved, err := article.Title.ResolveRequest(ctx)
	if err != nil {
		return value.Optional[string]{}, err
	}
	text, present := resolved.Get()
	if !present {
		return value.Optional[string]{}, nil
	}
	return value.Set(text.Text), nil
}

// doc-example:end resolve-slot-text
