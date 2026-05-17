package handler

import (
	"net/http"

	"code-rag-assistant/internal/service"

	"github.com/gin-gonic/gin"
)

type App struct {
	ingest *service.IngestService
	answer *service.AnswerService
	impact *service.ImpactService
}

func RegisterRoutes(router *gin.Engine, ingest *service.IngestService, answer *service.AnswerService, impact *service.ImpactService) {
	app := &App{ingest: ingest, answer: answer, impact: impact}
	router.GET("/healthz", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})
	api := router.Group("/api")
	api.POST("/repos", app.createRepository)
	api.POST("/repos/ensure", app.ensureRepository)
	api.GET("/repos/:id", app.getRepository)
	api.POST("/ask", app.ask)
	api.POST("/ask/stream", app.askStream)
	api.POST("/impact", app.impactAnalyze)
}
