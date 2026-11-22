package repository

import "errors"

var (
	ErrNotFound = errors.New("record not found")
	ErrEmailAlreadyExists = errors.New("User with this email already exists")
	ErrIntegrationAlreadyExists  = errors.New("integration for this provider already exists")
	ErrInvalidRole        = errors.New("invalid user role")
	ErrConstraintFailed   = errors.New("database constraint violation")
	ErrTimeout            = errors.New("database operation timed out")
)