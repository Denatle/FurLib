package archivator

import (
	"FurLib/internal/config"
	"os"

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
	return r.db.
		Where(Post{PostID: post.PostID, Source: post.Source}).
		FirstOrCreate(post).Error
}

func (r *Repository) Search(tags []string, limit int, scopes ...func(*gorm.DB) *gorm.DB) ([]Post, error) {
	db := r.db.Model(&Post{})
	for _, tag := range tags {
		db = db.Where("EXISTS (SELECT 1 FROM json_each(tags) WHERE value = ?)", tag)
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

func (r *Repository) UpdateFileInfo(id uint, path, hash string) error {
	return r.db.Model(&Post{}).Where("id = ?", id).Updates(map[string]any{
		"file_path":  path,
		"local_hash": hash,
	}).Error
}
