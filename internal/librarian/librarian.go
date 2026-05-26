package librarian

import (
	"FurLib/internal/archivator"

	"go.uber.org/zap"
)

type Librarian struct {
	repo *archivator.Repository
	log  *zap.Logger
}

func NewLibrarian(repo *archivator.Repository, log *zap.Logger) *Librarian {
	return &Librarian{repo: repo, log: log}
}

func (l *Librarian) Search(tags []string, limit int) ([]archivator.Post, error) {
	return l.repo.Search(tags, limit)
}

func (l *Librarian) GetPost(id uint) (*archivator.Post, error) {
	return l.repo.FindByID(id)
}
