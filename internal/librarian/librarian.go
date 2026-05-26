package librarian

import (
	"FurLib/internal/archivator"
	"context"

	"go.uber.org/zap"
	"gorm.io/gorm"
)

type Librarian struct {
	repo   *archivator.Repository
	healer *archivator.Healer
	log    *zap.Logger
}

func NewLibrarian(repo *archivator.Repository, healer *archivator.Healer, log *zap.Logger) *Librarian {
	return &Librarian{repo: repo, healer: healer, log: log}
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
	tags := f.Tags
	if f.Author != "" {
		tags = append([]string{f.Author}, tags...)
	}

	var scopes []func(*gorm.DB) *gorm.DB

	sortScope, ok := sortScopes[f.Sort]
	if !ok {
		sortScope = sortScopes["newest"]
	}
	scopes = append(scopes, sortScope)

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

	return l.repo.Search(tags, limit, scopes...)
}

func (l *Librarian) GetPost(id uint) (*archivator.Post, error) {
	return l.repo.FindByID(id)
}
