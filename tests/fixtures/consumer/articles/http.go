package articles

import (
	"context"
	"errors"

	"github.com/weiloon1234/Foundry-Go/attachments"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/query"
	"github.com/weiloon1234/Foundry-Go/extensions"
	"github.com/weiloon1234/Foundry-Go/extensions/slots"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/i18n"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/validation"
	"github.com/weiloon1234/Foundry-Go/value"
)

// ArticlePath addresses one article.
//
//foundry:path pattern=/articles/{article}
type ArticlePath struct {
	Article model.ID[Article]
}

// ArticleMediaPath addresses one article's media upload.
//
//foundry:path pattern=/articles/{article}/media
type ArticleMediaPath struct {
	Article model.ID[Article]
}

// ArticleInput creates or replaces an article's text. Translated fields are
// objects keyed by locale, for example {"en": "Hello", "ms": "Helo"}.
//
//foundry:dto
type ArticleInput struct {
	Slug    string                   `json:"slug"`
	Author  string                   `json:"author"`
	Title   map[i18n.LocaleID]string `json:"title"`
	Summary map[i18n.LocaleID]string `json:"summary"`
	SEO     value.Optional[SEO]      `json:"seo,omitzero"`
}

// Gallery is the multipart part carrying gallery images.
type Gallery []foundryhttp.UploadedFile

// ArticleMedia uploads a logo and gallery images, with optional translated
// text as a JSON part.
//
//foundry:multipart
type ArticleMedia struct {
	Title     value.Optional[map[i18n.LocaleID]string] `form:",json"`
	Logo      value.Optional[foundryhttp.UploadedFile]
	Galleries Gallery
}

// FileView presents one loaded attachment; attachments never serialize
// implicitly, so responses map the fields they disclose.
//
//foundry:dto role=response
type FileView struct {
	Name      string `json:"name"`
	MediaType string `json:"media_type"`
	Width     int    `json:"width"`
	Height    int    `json:"height"`
	Thumbnail bool   `json:"thumbnail"`
}

// ArticleView presents an article in the request's locale.
//
//foundry:dto role=response
type ArticleView struct {
	ID          model.ID[Article]        `json:"id"`
	Slug        string                   `json:"slug"`
	Title       string                   `json:"title"`
	TitleLocale i18n.LocaleID            `json:"title_locale"`
	Summary     value.Optional[string]   `json:"summary,omitzero"`
	Logo        value.Optional[FileView] `json:"logo,omitzero"`
	Galleries   []FileView               `json:"galleries"`
	SEO         value.Optional[SEO]      `json:"seo,omitzero"`
}

// ArticleList is one page of article views.
//
//foundry:dto role=response
type ArticleList struct {
	Items []ArticleView `json:"items"`
}

// Service maps typed requests to slot writes and loads. The fixture keeps its
// tables in the extension store's isolated schema, so the store supplies the
// transactions; an application with ordinary tables uses its database handle.
type Service struct {
	store *extensions.Store
	x     ArticleExtensionSlots
}

// NewService binds the extension runtime once.
func NewService(store *extensions.Store, runtime slots.Runtime) *Service {
	return &Service{store: store, x: ArticleExtensions().From(runtime)}
}

// Routes declares the article endpoints. Translated and file rules come from
// the model's slot policy, so validation and writes share one source.
func (s *Service) Routes() ([]foundryhttp.RouteRegistration, error) {
	input := ArticleInputValidationFields()
	textRules := validation.All(
		input.Slug.Rules(validation.NonBlank[string]()),
		input.Author.Rules(validation.NonBlank[string]()),
		input.Title.Rules(s.x.Title.Rule()),
		input.Summary.Rules(s.x.Summary.Rule()),
	)
	media := ArticleMediaValidationFields()
	logo := validation.Custom(validation.Spec{ID: "articles.logo", Message: "The logo is not an accepted image."}, func(ctx context.Context, file foundryhttp.UploadedFile) (bool, error) {
		return s.x.Logo.Accepts(ctx, file)
	})
	gallery := validation.Custom(validation.Spec{ID: "articles.gallery", Message: "The gallery image is not accepted."}, func(ctx context.Context, file foundryhttp.UploadedFile) (bool, error) {
		return s.x.Galleries.Accepts(ctx, file)
	})
	mediaRules := validation.All(
		media.Title.Rules(validation.Optional(s.x.Title.MergeRule())),
		media.Logo.Rules(validation.Optional(logo)),
		media.Galleries.Rules(validation.MaxItems[Gallery](s.x.Galleries.Describe().MaxFiles), validation.Each[Gallery](gallery)),
	)
	create := foundryhttp.DefineEndpoint(foundryhttp.DefineRoute(foundryhttp.RouteSpec{ID: "articles.create", Method: foundryhttp.POST, Access: foundryhttp.Public}, foundryhttp.StaticPath("/articles")), foundryhttp.EmptyQuery(), foundryhttp.JSONBody(ArticleInputJSON()), foundryhttp.JSONResponse(201, ArticleViewJSON())).WithBodyValidation(textRules)
	update := foundryhttp.DefineEndpoint(foundryhttp.DefineRoute(foundryhttp.RouteSpec{ID: "articles.update", Method: foundryhttp.PUT, Access: foundryhttp.Public}, ArticlePathDescriptor()), foundryhttp.EmptyQuery(), foundryhttp.JSONBody(ArticleInputJSON()), foundryhttp.JSONResponse(200, ArticleViewJSON())).WithBodyValidation(textRules)
	upload := foundryhttp.DefineEndpoint(foundryhttp.DefineRoute(foundryhttp.RouteSpec{ID: "articles.media", Method: foundryhttp.POST, Access: foundryhttp.Public}, ArticleMediaPathDescriptor()), foundryhttp.EmptyQuery(), foundryhttp.MultipartBody(ArticleMediaDescriptor()), foundryhttp.JSONResponse(200, ArticleViewJSON())).WithBodyValidation(mediaRules)
	show := foundryhttp.DefineEndpoint(foundryhttp.DefineRoute(foundryhttp.RouteSpec{ID: "articles.show", Method: foundryhttp.GET, Access: foundryhttp.Public}, ArticlePathDescriptor()), foundryhttp.EmptyQuery(), foundryhttp.EmptyBody(), foundryhttp.JSONResponse(200, ArticleViewJSON()))
	list := foundryhttp.DefineEndpoint(foundryhttp.DefineRoute(foundryhttp.RouteSpec{ID: "articles.list", Method: foundryhttp.GET, Access: foundryhttp.Public}, foundryhttp.StaticPath("/articles")), foundryhttp.EmptyQuery(), foundryhttp.EmptyBody(), foundryhttp.JSONResponse(200, ArticleListJSON()))
	remove := foundryhttp.DefineEndpoint(foundryhttp.DefineRoute(foundryhttp.RouteSpec{ID: "articles.delete", Method: foundryhttp.DELETE, Access: foundryhttp.Public}, ArticlePathDescriptor()), foundryhttp.EmptyQuery(), foundryhttp.EmptyBody(), foundryhttp.EmptyResponse(204))
	return []foundryhttp.RouteRegistration{
		create.Handle(s.Create), update.Handle(s.Update), upload.Handle(s.Upload),
		show.Handle(s.Show), list.Handle(s.List), remove.Handle(s.Delete),
	}, nil
}

// Create writes the article, its translated text and metadata in one
// transaction, then reads it back through the same transaction.
func (s *Service) Create(ctx context.Context, in foundryhttp.Input[foundryhttp.NoPath, foundryhttp.NoQuery, ArticleInput]) (ArticleView, error) {
	var article Article
	err := s.store.Write(ctx, func(ctx context.Context, tx *database.Tx) error {
		author, err := QueryArticleAuthors().Create(ctx, tx, AuthorDraft{}.SetName(in.Body.Author))
		if err != nil {
			return err
		}
		created, err := QueryArticles().Create(ctx, tx, ArticleDraft{}.SetSlug(in.Body.Slug).SetAuthorID(author.ID))
		if err != nil {
			return err
		}
		if err := s.writeText(ctx, tx, created, in.Body); err != nil {
			return err
		}
		article, err = QueryArticles().With(s.x.All()...).RequireFind(ctx, tx, created.ID)
		return err
	})
	if err != nil {
		return ArticleView{}, err
	}
	return s.present(ctx, article)
}

// Update replaces the article's text: SyncIn removes locales the request no
// longer sends, so an edit form sending every locale is authoritative.
func (s *Service) Update(ctx context.Context, in foundryhttp.Input[ArticlePath, foundryhttp.NoQuery, ArticleInput]) (ArticleView, error) {
	var article Article
	err := s.store.Write(ctx, func(ctx context.Context, tx *database.Tx) error {
		existing, err := QueryArticles().RequireFind(ctx, tx, in.Path.Article)
		if err != nil {
			return err
		}
		updated, err := QueryArticles().Update(ctx, tx, existing.ID, ArticleDraft{}.SetSlug(in.Body.Slug))
		if err != nil {
			return err
		}
		if err := s.writeText(ctx, tx, updated, in.Body); err != nil {
			return err
		}
		article, err = QueryArticles().With(s.x.All()...).RequireFind(ctx, tx, updated.ID)
		return err
	})
	if err != nil {
		return ArticleView{}, notFound(err)
	}
	return s.present(ctx, article)
}

func (s *Service) writeText(ctx context.Context, tx *database.Tx, article Article, in ArticleInput) error {
	if err := s.x.Title.SyncIn(ctx, tx, article, in.Title); err != nil {
		return err
	}
	if err := s.x.Summary.SyncIn(ctx, tx, article, in.Summary); err != nil {
		return err
	}
	if seo, ok := in.SEO.Get(); ok {
		return s.x.SEO.SaveIn(ctx, tx, article, seo)
	}
	_, err := s.x.SEO.ForgetIn(ctx, tx, article)
	return err
}

// Upload publishes files after the article exists. Attachments publish
// outside the text transaction; a failure leaves earlier files published.
func (s *Service) Upload(ctx context.Context, in foundryhttp.Input[ArticleMediaPath, foundryhttp.NoQuery, ArticleMedia]) (ArticleView, error) {
	article, err := s.find(ctx, in.Path.Article)
	if err != nil {
		return ArticleView{}, err
	}
	if title, ok := in.Body.Title.Get(); ok {
		if err := s.x.Title.Save(ctx, article, title); err != nil {
			return ArticleView{}, err
		}
	}
	if logo, ok := in.Body.Logo.Get(); ok {
		if _, err := s.x.Logo.ReplaceFile(ctx, article, logo); err != nil {
			return ArticleView{}, err
		}
	}
	if _, err := attachments.AddFiles(ctx, s.x.Galleries, article, in.Body.Galleries); err != nil {
		return ArticleView{}, err
	}
	return s.Show(ctx, foundryhttp.Input[ArticlePath, foundryhttp.NoQuery, foundryhttp.NoBody]{Path: ArticlePath{Article: in.Path.Article}})
}

// Show loads every slot with the article.
func (s *Service) Show(ctx context.Context, in foundryhttp.Input[ArticlePath, foundryhttp.NoQuery, foundryhttp.NoBody]) (ArticleView, error) {
	article, err := s.find(ctx, in.Path.Article, s.x.All()...)
	if err != nil {
		return ArticleView{}, err
	}
	return s.present(ctx, article)
}

// List loads the titles, logos and galleries a listing displays.
func (s *Service) List(ctx context.Context, _ foundryhttp.Input[foundryhttp.NoPath, foundryhttp.NoQuery, foundryhttp.NoBody]) (ArticleList, error) {
	var list []Article
	if err := s.store.Read(ctx, func(ctx context.Context, tx *database.Tx) error {
		var err error
		list, err = QueryArticles().With(s.x.Title, s.x.Summary, s.x.Logo, s.x.Galleries, s.x.SEO).OrderBy(ArticleFields().Slug.Asc()).All(ctx, tx)
		return err
	}); err != nil {
		return ArticleList{}, err
	}
	result := ArticleList{Items: make([]ArticleView, len(list))}
	for i, article := range list {
		var err error
		if result.Items[i], err = s.present(ctx, article); err != nil {
			return ArticleList{}, err
		}
	}
	return result, nil
}

// Delete removes the article; generated cleanup removes its extension data.
func (s *Service) Delete(ctx context.Context, in foundryhttp.Input[ArticlePath, foundryhttp.NoQuery, foundryhttp.NoBody]) (foundryhttp.NoContent, error) {
	err := s.store.Write(ctx, func(ctx context.Context, tx *database.Tx) error {
		_, err := QueryArticles().ForceDelete(ctx, tx, in.Path.Article)
		return err
	})
	return foundryhttp.NoContent{}, notFound(err)
}

func (s *Service) find(ctx context.Context, id model.ID[Article], relations ...query.Relation[Article]) (Article, error) {
	var article Article
	err := s.store.Read(ctx, func(ctx context.Context, tx *database.Tx) error {
		var err error
		article, err = QueryArticles().With(relations...).RequireFind(ctx, tx, id)
		return err
	})
	return article, notFound(err)
}

// present maps loaded slots to the response in the request's locale. Reads
// never perform I/O.
func (s *Service) present(ctx context.Context, article Article) (ArticleView, error) {
	view := ArticleView{ID: article.ID, Slug: article.Slug, Galleries: []FileView{}}
	title, err := article.Title.ResolveRequest(ctx)
	if err != nil {
		return ArticleView{}, err
	}
	if resolved, ok := title.Get(); ok {
		view.Title, view.TitleLocale = resolved.Text, resolved.Locale
	}
	summary, err := article.Summary.ResolveRequest(ctx)
	if err != nil {
		return ArticleView{}, err
	}
	if resolved, ok := summary.Get(); ok {
		view.Summary = value.Set(resolved.Text)
	}
	if logo, _ := article.Logo.Get(); logo.IsSet() {
		file, _ := logo.Get()
		view.Logo = value.Set(fileView(file))
	}
	for _, file := range article.Galleries.All() {
		view.Galleries = append(view.Galleries, fileView(file))
	}
	if seo, _ := article.SEO.Get(); seo.IsSet() {
		view.SEO = seo
	}
	return view, nil
}

func fileView(file attachments.File[Article]) FileView {
	info := file.Info()
	_, thumbnail := file.Variant(Thumbnail)
	return FileView{Name: info.OriginalName, MediaType: string(info.MediaType), Width: info.Width, Height: info.Height, Thumbnail: thumbnail}
}

func notFound(err error) error {
	if errors.Is(err, database.NotFound) {
		return foundryhttp.NotFound.WithCause(err)
	}
	return err
}
