package controller

import (
	"errors"
	"regexp"

	"unified-tx-parser/internal/api/service"

	"github.com/gin-gonic/gin"
)

var hashPattern = regexp.MustCompile(`^(0x)?[a-fA-F0-9]{64}$`)

type TransactionController struct {
	svc *service.TransactionService
}

func NewTransactionController(svc *service.TransactionService) *TransactionController {
	return &TransactionController{svc: svc}
}

func (ctrl *TransactionController) GetByHash(c *gin.Context) {
	hash := c.Param("hash")
	if hash == "" {
		badRequest(c, "transaction hash is required")
		return
	}
	if !hashPattern.MatchString(hash) && len(hash) < 20 {
		badRequest(c, "invalid transaction hash format")
		return
	}

	tx, err := ctrl.svc.GetByHash(c.Request.Context(), hash)
	if err != nil {
		if errors.Is(err, service.ErrTransactionNotFound) {
			notFound(c, "transaction not found")
			return
		}
		internalError(c, err.Error())
		return
	}

	success(c, gin.H{"transaction": tx})
}
