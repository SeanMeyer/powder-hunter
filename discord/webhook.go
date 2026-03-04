package discord

import (
	"context"

	"github.com/seanmeyer/powder-hunter/domain"
)

// Poster publishes storm evaluations to Discord. PostNew opens a new thread and
// returns its ID; PostUpdate appends to an existing thread.
type Poster interface {
	PostNew(ctx context.Context, eval domain.Evaluation, region domain.Region) (threadID string, err error)
	PostUpdate(ctx context.Context, eval domain.Evaluation, region domain.Region, threadID string) error
}
