package main

import (
	"context"
	"log"
	"net/http"

	"code-rag-assistant/internal/config"
	"code-rag-assistant/internal/handler"
	"code-rag-assistant/internal/model"
	dbrepo "code-rag-assistant/internal/repository"
	"code-rag-assistant/internal/service"

	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
)

func main() {
	cfg := config.Load()

	db, err := dbrepo.OpenPostgres(cfg.PostgresDSN)
	if err != nil {
		log.Fatalf("open postgres: %v", err)
	}
	if err := db.Exec("CREATE EXTENSION IF NOT EXISTS vector").Error; err != nil {
		log.Fatalf("create pgvector extension: %v", err)
	}
	if err := db.AutoMigrate(&model.Repository{}, &model.CodeChunk{}); err != nil {
		log.Fatalf("auto migrate: %v", err)
	}
	if err := db.Exec("ALTER TABLE code_chunks ADD COLUMN IF NOT EXISTS search_vector tsvector").Error; err != nil {
		log.Fatalf("add search vector column: %v", err)
	}
	if err := db.Exec("UPDATE code_chunks SET search_vector = " + model.CodeChunkSearchVectorExpression + " WHERE search_vector IS NULL").Error; err != nil {
		log.Printf("backfill search vector: %v", err)
	}
	if err := db.Exec("CREATE INDEX IF NOT EXISTS idx_code_chunks_search_vector ON code_chunks USING GIN (search_vector)").Error; err != nil {
		log.Printf("create search vector index: %v", err)
	}
	if err := db.Exec("CREATE INDEX IF NOT EXISTS idx_code_chunks_embedding_vector ON code_chunks USING hnsw (embedding_vector vector_cosine_ops)").Error; err != nil {
		log.Printf("create vector index: %v", err)
	}

	var rdb *redis.Client
	if cfg.RedisAddr != "" {
		rdb = redis.NewClient(&redis.Options{
			Addr:     cfg.RedisAddr,
			Password: cfg.RedisPassword,
			DB:       cfg.RedisDB,
		})
		if err := rdb.Ping(context.Background()).Err(); err != nil {
			log.Printf("redis unavailable, repository status cache disabled: %v", err)
			_ = rdb.Close()
			rdb = nil
		}
	}

	embedder := service.NewEmbedder(cfg)
	fetcher := service.NewGitHubFetcher(cfg)
	indexer := service.NewCodeIndexer(db, embedder, cfg)
	ingest := service.NewIngestService(db, rdb, fetcher, indexer, cfg)
	retriever := service.NewRetriever(db, embedder, cfg)
	answer := service.NewAnswerService(db, retriever, cfg)
	impact := service.NewImpactService(db, retriever, cfg)

	router := gin.New()
	router.Use(gin.Logger())
	router.Use(gin.CustomRecovery(func(c *gin.Context, recovered any) {
		log.Printf("panic: %v", recovered)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
	}))
	handler.RegisterRoutes(router, ingest, answer, impact)
	router.Static("/assets", "./static/assets")
	router.StaticFile("/favicon.png", "./static/favicon.png")
	router.StaticFile("/", "./static/index.html")

	log.Printf("code-rag-assistant listening on http://127.0.0.1:%s", cfg.Port)
	if err := router.Run(":" + cfg.Port); err != nil {
		log.Fatalf("run server: %v", err)
	}
}
