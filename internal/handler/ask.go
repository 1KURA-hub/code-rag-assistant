package handler

import (
	"encoding/json"
	"net/http"

	"code-rag-assistant/internal/service"

	"github.com/gin-gonic/gin"
)

type askRequest struct {
	RepositoryID uint   `json:"repository_id"`
	Question     string `json:"question"`
}

func (a *App) ask(c *gin.Context) {
	var req askRequest
	if err := c.ShouldBindJSON(&req); err != nil || req.RepositoryID == 0 || req.Question == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "repository_id and question are required"})
		return
	}
	resp, err := a.answer.Ask(c.Request.Context(), req.RepositoryID, req.Question)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, resp)
}

func (a *App) askStream(c *gin.Context) {
	var req askRequest
	if err := c.ShouldBindJSON(&req); err != nil || req.RepositoryID == 0 || req.Question == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "repository_id and question are required"})
		return
	}
	flusher, ok := c.Writer.(http.Flusher)
	if !ok {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "streaming is not supported"})
		return
	}
	c.Header("Content-Type", "application/x-ndjson; charset=utf-8")
	c.Header("Cache-Control", "no-cache")
	c.Header("X-Accel-Buffering", "no")
	c.Status(http.StatusOK)

	encoder := json.NewEncoder(c.Writer)
	emit := func(event service.AskStreamEvent) error {
		if err := encoder.Encode(event); err != nil {
			return err
		}
		flusher.Flush()
		return nil
	}
	if err := a.answer.AskStream(c.Request.Context(), req.RepositoryID, req.Question, emit); err != nil {
		_ = emit(service.AskStreamEvent{Type: "error", Error: err.Error()})
	}
}
