package handler

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
)

type createRepoRequest struct {
	RepoURL string `json:"repo_url"`
}

func (a *App) createRepository(c *gin.Context) {
	var req createRepoRequest
	if err := c.ShouldBindJSON(&req); err != nil || req.RepoURL == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "repo_url is required"})
		return
	}
	repo, err := a.ingest.CreateAndIndex(c.Request.Context(), req.RepoURL)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, repo)
}

func (a *App) ensureRepository(c *gin.Context) {
	var req createRepoRequest
	if err := c.ShouldBindJSON(&req); err != nil || req.RepoURL == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "repo_url is required"})
		return
	}
	repo, err := a.ingest.Ensure(c.Request.Context(), req.RepoURL)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, repo)
}

func (a *App) getRepository(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid repository id"})
		return
	}
	repo, err := a.ingest.Get(c.Request.Context(), uint(id))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "repository not found"})
		return
	}
	c.JSON(http.StatusOK, repo)
}
