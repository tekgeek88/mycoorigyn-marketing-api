package pageviews

import (
	"context"
	"errors"
	"strings"
)

const (
	defaultPagePath = "landing"
	maxPagePathLen  = 200
)

var (
	errPagePathTooLong = errors.New("page is too long")
)

type Repository interface {
	RecordVisit(ctx context.Context, pagePath string, visitorID string) (VisitorCounter, error)
	GetCounts(ctx context.Context, pagePath string) (VisitorCounter, error)
}

type CounterService interface {
	Record(ctx context.Context, req RecordPageViewRequest) (VisitorCounter, error)
	Get(ctx context.Context, pagePath string) (VisitorCounter, error)
}

type Service struct {
	repo Repository
}

type ServiceOptions struct{}

type RecordPageViewRequest struct {
	Page      string `json:"page"`
	VisitorID string `json:"visitor_id"`
}

type ValidationError struct {
	Message string
}

func (e ValidationError) Error() string {
	if e.Message == "" {
		return "validation error"
	}
	return e.Message
}

type VisitorCounter struct {
	Page           string `json:"page"`
	TotalVisits    int64  `json:"total_visits"`
	UniqueVisitors int64  `json:"unique_visitors"`
}

func NewService(repo Repository, _ ServiceOptions) Service {
	return Service{repo: repo}
}

func (s Service) Record(ctx context.Context, req RecordPageViewRequest) (VisitorCounter, error) {
	page, err := normalizePagePath(req.Page)
	if err != nil {
		return VisitorCounter{}, ValidationError{Message: err.Error()}
	}

	return s.repo.RecordVisit(ctx, page, strings.TrimSpace(req.VisitorID))
}

func (s Service) Get(ctx context.Context, pagePath string) (VisitorCounter, error) {
	page, err := normalizePagePath(pagePath)
	if err != nil {
		return VisitorCounter{}, ValidationError{Message: err.Error()}
	}

	return s.repo.GetCounts(ctx, page)
}

func normalizePagePath(page string) (string, error) {
	page = strings.TrimSpace(page)
	if page == "" {
		return defaultPagePath, nil
	}

	if len(page) > maxPagePathLen {
		return "", errPagePathTooLong
	}

	return page, nil
}
