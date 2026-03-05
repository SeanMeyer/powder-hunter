package discord

import (
	"context"
	"fmt"

	"github.com/seanmeyer/powder-hunter/domain"
)

// FakePoster records posted embeds for testing assertions.
type FakePoster struct {
	PostedBriefings []PostBriefingCall
	PostedDetails   []PostDetailCall
	PostedUpdates   []PostUpdateCall
	NextThreadID    string
	PostBriefingError error
	PostDetailError   error
	PostUpdateError   error
}

// PostBriefingCall captures the arguments of a single PostBriefing invocation.
type PostBriefingCall struct {
	BriefingPost BriefingPost
}

// PostDetailCall captures the arguments of a single PostDetail invocation.
type PostDetailCall struct {
	Evaluation domain.Evaluation
	Region     domain.Region
	ThreadID   string
}

// PostUpdateCall captures the arguments of a single PostUpdate invocation.
type PostUpdateCall struct {
	Evaluation domain.Evaluation
	Region     domain.Region
	ThreadID   string
}

func (f *FakePoster) PostBriefing(ctx context.Context, bp BriefingPost) (string, error) {
	f.PostedBriefings = append(f.PostedBriefings, PostBriefingCall{BriefingPost: bp})
	if f.PostBriefingError != nil {
		return "", f.PostBriefingError
	}
	tid := f.NextThreadID
	if tid == "" {
		tid = fmt.Sprintf("thread-%d", len(f.PostedBriefings))
	}
	return tid, nil
}

func (f *FakePoster) PostDetail(ctx context.Context, eval domain.Evaluation, region domain.Region, threadID string) error {
	f.PostedDetails = append(f.PostedDetails, PostDetailCall{Evaluation: eval, Region: region, ThreadID: threadID})
	return f.PostDetailError
}

func (f *FakePoster) PostUpdate(ctx context.Context, eval domain.Evaluation, region domain.Region, threadID string) error {
	f.PostedUpdates = append(f.PostedUpdates, PostUpdateCall{Evaluation: eval, Region: region, ThreadID: threadID})
	return f.PostUpdateError
}
