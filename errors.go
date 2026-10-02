package main

import (
	"errors"
	"fmt"
	"log"

	"github.com/labstack/echo/v4"
)

type Problem struct {
	Code   string         `json:"code"`
	Params map[string]any `json:"params,omitempty"`
}

func problem(code string) *Problem { return &Problem{Code: code} }
func (p *Problem) Error() string   { return p.Code }
func (p *Problem) With(key string, value any) *Problem {
	if p.Params == nil {
		p.Params = map[string]any{}
	}
	p.Params[key] = value
	return p
}

func asProblem(err error) *Problem {
	var p *Problem
	if errors.As(err, &p) {
		return p
	}
	return problem("invalid_value").With("detail", err.Error())
}

func handleError(err error, c echo.Context) {
	if c.Response().Committed {
		return
	}
	status := 500
	var httpError *echo.HTTPError
	if errors.As(err, &httpError) {
		status = httpError.Code
		if cause, ok := httpError.Message.(error); ok {
			err = cause
		}
	}
	var p *Problem
	if !errors.As(err, &p) {
		p = problem(fmt.Sprintf("http_%d", status)).With("status", status)
		if status == 500 {
			log.Printf("request failed: %v", err)
		}
	}
	_ = c.JSON(status, p)
}
