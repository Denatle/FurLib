package librarian

import (
	"FurLib/internal/archivator"
	"context"
	"strings"

	"go.uber.org/zap"
	"gorm.io/gorm"
)

type Librarian struct {
	repo       *archivator.Repository
	archivator *archivator.Archivator
	healer     *archivator.Healer
	log        *zap.Logger
}

func NewLibrarian(repo *archivator.Repository, arch *archivator.Archivator, healer *archivator.Healer, log *zap.Logger) *Librarian {
	return &Librarian{repo: repo, archivator: arch, healer: healer, log: log}
}

func (l *Librarian) Check() (archivator.HealthReport, error) {
	return l.healer.Check()
}

func (l *Librarian) Heal(ctx context.Context) (archivator.HealReport, error) {
	return l.healer.Heal(ctx)
}

var sortScopes = map[string]func(*gorm.DB) *gorm.DB{
	"newest": func(db *gorm.DB) *gorm.DB { return db.Order("post_created_at DESC") },
	"oldest": func(db *gorm.DB) *gorm.DB { return db.Order("post_created_at ASC") },
	"score":  func(db *gorm.DB) *gorm.DB { return db.Order("score DESC") },
}

type SearchFilters struct {
	Tags     []string
	Author   string
	Sort     string
	Animated *bool    // nil = all, true = animated only, false = static only
	Ratings  []string // nil/empty = all; values: "safe", "questionable", "explicit"
}

func (l *Librarian) Search(limit int, f SearchFilters) ([]archivator.Post, error) {
	var scopes []func(*gorm.DB) *gorm.DB

	sortScope, ok := sortScopes[f.Sort]
	if !ok {
		sortScope = sortScopes["newest"]
	}
	scopes = append(scopes, sortScope)

	if f.Author != "" {
		a := strings.ToLower(f.Author)
		scopes = append(scopes, func(db *gorm.DB) *gorm.DB {
			// Match Author column OR fall back to tag-based artist search for older posts.
			return db.Where(
				"LOWER(author) LIKE ? OR EXISTS (SELECT 1 FROM json_each(tags) WHERE LOWER(value) LIKE ?)",
				"%"+a+"%", "%"+a+"%",
			)
		})
	}

	if f.Animated != nil {
		v := *f.Animated
		scopes = append(scopes, func(db *gorm.DB) *gorm.DB {
			return db.Where("animated = ?", v)
		})
	}

	if len(f.Ratings) > 0 {
		scopes = append(scopes, func(db *gorm.DB) *gorm.DB {
			return db.Where("rating IN ?", f.Ratings)
		})
	}

	posts, err := l.repo.Search(f.Tags, limit, scopes...)
	if err != nil {
		return nil, err
	}
	// Lazy backfill: persist Author for older posts that matched via the tag fallback.
	if f.Author != "" {
		for i := range posts {
			if posts[i].Author == "" {
				posts[i].Author = f.Author
				_ = l.repo.UpdateAuthor(posts[i].ID, f.Author)
			}
		}
	}
	return posts, nil
}

func (l *Librarian) GetPost(id uint) (*archivator.Post, error) {
	return l.repo.FindByID(id)
}

func (l *Librarian) SoftDelete(id uint) error {
	return l.archivator.SoftDelete(id)
}

func (l *Librarian) ListDeleted(limit int) ([]archivator.Post, error) {
	return l.repo.ListDeleted(limit, sortScopes["newest"])
}

func (l *Librarian) ClearDeleted() (int64, error) {
	return l.repo.ClearDeleted()
}
