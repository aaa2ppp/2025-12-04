package model

import "errors"

var (
	ErrNotFound      = errors.New("not found")
	ErrInternalError = errors.New("internal error")
)
