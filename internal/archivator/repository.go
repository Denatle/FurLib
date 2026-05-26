package archivator

import (
	"FurLib/internal/config"
	"os"
	"strings"

	"github.com/glebarez/sqlite"
	"go.uber.org/zap"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

type Repository struct {
	db *gorm.DB
}

func NewRepository(log *zap.Logger, cfg config.ArchivatorConfig) (*Repository, error) {
	db, err := gorm.Open(sqlite.Open(cfg.DBPath), &gorm.Config{
		Logger: gormlogger.Default.LogMode(gormlogger.Silent),
	})
	if err != nil {
		return nil, err
	}

	if err := db.AutoMigrate(&Post{}); err != nil {
		return nil, err
	}

	log.Info("archivator database ready", zap.String("path", cfg.DBPath))
	return &Repository{db: db}, nil
}

func (r *Repository) Save(post *Post) error {
	var existing Post
	// Use Unscoped so soft-deleted records are found and skipped.
	// Without this, a deleted post would be re-created on next download.
	err := r.db.Unscoped().
		Where("post_id = ? AND source = ?", post.PostID, post.Source).
		First(&existing).Error
	if err == nil {
		return nil // already exists (possibly soft-deleted) — skip
	}
	return r.db.Create(post).Error
}

func (r *Repository) Search(tags []string, limit int, scopes ...func(*gorm.DB) *gorm.DB) ([]Post, error) {
	db := r.db.Model(&Post{})
	for _, tag := range tags {
		if strings.HasPrefix(tag, "-") {
			neg := tag[1:]
			db = db.Where("NOT EXISTS (SELECT 1 FROM json_each(tags) WHERE value LIKE ?)", "%"+neg+"%")
		} else {
			db = db.Where("EXISTS (SELECT 1 FROM json_each(tags) WHERE value LIKE ?)", "%"+tag+"%")
		}
	}
	for _, scope := range scopes {
		db = scope(db)
	}
	var posts []Post
	err := db.Limit(limit).Find(&posts).Error
	return posts, err
}

func (r *Repository) FindByID(id uint) (*Post, error) {
	var post Post
	err := r.db.First(&post, id).Error
	return &post, err
}

func (r *Repository) FindAll() ([]*Post, error) {
	var posts []*Post
	return posts, r.db.Find(&posts).Error
}

func (r *Repository) FindMissing() ([]*Post, error) {
	all, err := r.FindAll()
	if err != nil {
		return nil, err
	}
	var missing []*Post
	for _, p := range all {
		if _, err := os.Stat(p.FilePath); os.IsNotExist(err) {
			missing = append(missing, p)
		}
	}
	return missing, nil
}

func (r *Repository) UpdateAuthor(id uint, author string) error {
	return r.db.Model(&Post{}).Where("id = ?", id).Update("author", author).Error
}

func (r *Repository) UpdateFileInfo(id uint, path, hash string, size uint64) error {
	return r.db.Model(&Post{}).Where("id = ?", id).Updates(map[string]any{
		"file_path":  path,
		"local_hash": hash,
		"size":       size,
	}).Error
}

// SoftDelete marks a post as deleted and returns its file path so the caller can
// remove the file from disk. GORM sets deleted_at; future Save calls will skip it.
func (r *Repository) SoftDelete(id uint) (*Post, error) {
	post, err := r.FindByID(id)
	if err != nil {
		return nil, err
	}
	return post, r.db.Delete(post).Error
}

// ListDeleted returns posts that have been soft-deleted.
func (r *Repository) ListDeleted(limit int, scopes ...func(*gorm.DB) *gorm.DB) ([]Post, error) {
	db := r.db.Unscoped().Model(&Post{}).Where("deleted_at IS NOT NULL")
	for _, s := range scopes {
		db = s(db)
	}
	var posts []Post
	return posts, db.Limit(limit).Find(&posts).Error
}

// ClearDeleted permanently removes all soft-deleted records from the database.
func (r *Repository) ClearDeleted() (int64, error) {
	result := r.db.Unscoped().Where("deleted_at IS NOT NULL").Delete(&Post{})
	return result.RowsAffected, result.Error
}
