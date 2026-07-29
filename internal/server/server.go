package server

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"auto-router/internal/config"
)

type Config = config.Config

func NewRouter(_ Config) *gin.Engine {
	r := gin.New()
	r.Use(gin.Recovery())
	r.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})
	return r
}
