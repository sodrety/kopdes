package app

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
)

var errInvalidRupiahAmount = errors.New("invalid Rupiah amount")

func parseRupiahAmount(raw string) (int64, error) {
	value := strings.TrimSpace(raw)
	if len(value) >= 2 && strings.EqualFold(value[:2], "rp") {
		value = strings.TrimSpace(value[2:])
	}
	if value == "" || strings.ContainsAny(value, " \t\r\n") {
		return 0, errInvalidRupiahAmount
	}

	digits := value
	hasDot := strings.Contains(value, ".")
	hasComma := strings.Contains(value, ",")
	if hasDot && hasComma {
		return 0, errInvalidRupiahAmount
	}
	if hasDot || hasComma {
		separator := "."
		if hasComma {
			separator = ","
		}
		groups := strings.Split(value, separator)
		if len(groups) < 2 || len(groups[0]) < 1 || len(groups[0]) > 3 || !asciiDigits(groups[0]) {
			return 0, errInvalidRupiahAmount
		}
		for _, group := range groups[1:] {
			if len(group) != 3 || !asciiDigits(group) {
				return 0, errInvalidRupiahAmount
			}
		}
		digits = strings.Join(groups, "")
	} else if !asciiDigits(value) {
		return 0, errInvalidRupiahAmount
	}

	amount, err := strconv.ParseInt(digits, 10, 64)
	if err != nil {
		return 0, errInvalidRupiahAmount
	}
	return amount, nil
}

func asciiDigits(value string) bool {
	if value == "" {
		return false
	}
	for _, character := range value {
		if character < '0' || character > '9' {
			return false
		}
	}
	return true
}

func bindRequestWithRupiahAmount(c *gin.Context, destination any, field string) error {
	if !isBrowserFormRequest(c) {
		return c.ShouldBind(destination)
	}
	if err := c.Request.ParseForm(); err != nil {
		return err
	}
	rawAmount := c.Request.PostForm.Get(field)
	if strings.TrimSpace(rawAmount) == "" {
		rawAmount = "0"
	}
	amount, err := parseRupiahAmount(rawAmount)
	if err != nil {
		return err
	}
	if amount <= 0 {
		return errInvalidRupiahAmount
	}
	normalized := strconv.FormatInt(amount, 10)
	c.Request.PostForm.Set(field, normalized)
	c.Request.Form.Set(field, normalized)
	return c.ShouldBind(destination)
}

func invalidRupiahAmountResponse(c *gin.Context) {
	respondError(c, http.StatusBadRequest, "VALIDATION_ERROR", "error_invalid_rupiah_amount")
}

func isMonetaryAggregateCapacityError(err error) bool {
	return err != nil && strings.Contains(strings.ToLower(err.Error()), "monetary aggregate capacity exceeded")
}
