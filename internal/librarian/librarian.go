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

func (l *Librarian) Search(tags []string, limit int, sort string) ([]archivator.Post, error) {
	scope, ok := sortScopes[sort]
	if !ok {
		scope = sortScopes["newest"]
	}
	return l.repo.Search(tags, limit, scope)
}

func (l *Librarian) GetPost(id uint) (*archivator.Post, error) {
	return l.repo.FindByID(id)
}
