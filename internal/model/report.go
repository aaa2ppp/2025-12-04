package model

import "io"

type Report struct {
	ContentType string
	Reader      io.Reader
}
