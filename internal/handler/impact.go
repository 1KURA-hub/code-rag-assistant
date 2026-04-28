package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

type impactRequest struct {
	RepositoryID uint   `json:"repository_id"`
	DiffText     string `json:"diff_text"`
}

func (a *App) impactAnalyze(c *gin.Context) {
	var req impactRequest
	if err := c.ShouldBindJSON(&req); err != nil || req.RepositoryID == 0 || req.DiffText == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "repository_id and diff_text are required"})
		return
	}
	resp, err := a.impact.Analyze(c.Request.Context(), req.RepositoryID, req.DiffText)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, resp)
}
