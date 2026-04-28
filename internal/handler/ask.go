package handler

import (
	"net/http"

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
