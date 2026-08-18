package v1api

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/james-see/everydayisaholiday/api/internal/bearer"
	"github.com/james-see/everydayisaholiday/api/internal/digest"
)

type Handler struct {
	Holidays  *digest.Store
	Validator *bearer.Validator
}

func (h *Handler) Middleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		p, err := h.Validator.FromRequest(c.Request)
		if err != nil {
			c.Header("WWW-Authenticate", `Bearer realm="adayisaholiday"`)
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
			return
		}
		if err := h.Validator.CheckRateLimit(p); err != nil {
			if errors.Is(err, bearer.ErrRateLimited) {
				c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{"error": "rate_limit_exceeded"})
				return
			}
		}
		c.Set("principal", p)
		c.Next()
	}
}

// Today godoc
// @Summary Holidays for today (server local date UTC unless tz query)
// @Tags v1
// @Produce json
// @Param tz query string false "IANA timezone"
// @Success 200 {object} map[string]any
// @Security BearerAuth
// @Router /v1/today [get]
func (h *Handler) Today(c *gin.Context) {
	loc := time.UTC
	if tz := c.Query("tz"); tz != "" {
		if l, err := time.LoadLocation(tz); err == nil {
			loc = l
		}
	}
	now := time.Now().In(loc)
	list := h.Holidays.ForDate(int(now.Month()), now.Day(), nil)
	c.JSON(http.StatusOK, gin.H{
		"date":     now.Format("01-02"),
		"timezone": loc.String(),
		"count":    len(list),
		"holidays": list,
	})
}

// ByDate godoc
// @Summary Holidays for MM-DD
// @Tags v1
// @Produce json
// @Param mmdd path string true "MM-DD"
// @Success 200 {object} map[string]any
// @Security BearerAuth
// @Router /v1/holidays/{mmdd} [get]
func (h *Handler) ByDate(c *gin.Context) {
	mmdd := c.Param("mmdd")
	parts := strings.Split(mmdd, "-")
	if len(parts) != 2 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "date must be MM-DD"})
		return
	}
	month, err1 := strconv.Atoi(parts[0])
	day, err2 := strconv.Atoi(parts[1])
	if err1 != nil || err2 != nil || month < 1 || month > 12 || day < 1 || day > 31 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid date"})
		return
	}
	list := h.Holidays.ForDate(month, day, nil)
	c.JSON(http.StatusOK, gin.H{"date": mmdd, "count": len(list), "holidays": list})
}

// List godoc
// @Summary Search / list holidays
// @Tags v1
// @Produce json
// @Param q query string false "search"
// @Param category query string false "category"
// @Param date query string false "MM-DD"
// @Param limit query int false "max results"
// @Success 200 {object} map[string]any
// @Security BearerAuth
// @Router /v1/holidays [get]
func (h *Handler) List(c *gin.Context) {
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "50"))
	list := h.Holidays.Search(c.Query("q"), c.Query("category"), c.Query("date"), limit)
	c.JSON(http.StatusOK, gin.H{"count": len(list), "holidays": list, "total": h.Holidays.Count()})
}
