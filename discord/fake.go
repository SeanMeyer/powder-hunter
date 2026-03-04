package discord

import (
	"context"
	"fmt"

	"github.com/seanmeyer/powder-hunter/domain"
)

// FakePoster records posted embeds for testing assertions.
type FakePoster struct {
	// PostedNew records all PostNew calls.
	PostedNew []PostNewCall
	// PostedUpdates records all PostUpdate calls.
	PostedUpdates []PostUpdateCall
	// NextThreadID is returned by PostNew.
	NextThreadID string
	// PostNewError is returned by PostNew if set.
	PostNewError error
	// PostUpdateError is returned by PostUpdate if set.
	PostUpdateError error
}

// PostNewCall captures the arguments of a single PostNew invocation.
type PostNewCall struct {
	Evaluation domain.Evaluation
	Region     domain.Region
}

// PostUpdateCall captures the arguments of a single PostUpdate invocation.
type PostUpdateCall struct {
	Evaluation domain.Evaluation
	Region     domain.Region
	ThreadID   string
}

func (f *FakePoster) PostNew(ctx context.Context, eval domain.Evaluation, region domain.Region) (string, error) {
	f.PostedNew = append(f.PostedNew, PostNewCall{Evaluation: eval, Region: region})
	if f.PostNewError != nil {
		return "", f.PostNewError
	}
	tid := f.NextThreadID
	if tid == "" {
		tid = fmt.Sprintf("thread-%d", len(f.PostedNew))
	}
	return tid, nil
}

func (f *FakePoster) PostUpdate(ctx context.Context, eval domain.Evaluation, region domain.Region, threadID string) error {
	f.PostedUpdates = append(f.PostedUpdates, PostUpdateCall{Evaluation: eval, Region: region, ThreadID: threadID})
	return f.PostUpdateError
}
