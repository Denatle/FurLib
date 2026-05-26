package archivator

import (
	"FurLib/internal/config"

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

func (r *Repository) Search(tags []string, limit int) ([]Post, error) {
	db := r.db.Model(&Post{})
	for _, tag := range tags {
		db = db.Where("EXISTS (SELECT 1 FROM json_each(tags) WHERE value = ?)", tag)
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
