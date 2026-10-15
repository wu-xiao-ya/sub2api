package service

import "context"

type openAI403HandlingState struct {
	modelScoped bool
}

type openAI403HandlingStateContextKey struct{}

func withOpenAI403HandlingState(ctx context.Context) (context.Context, *openAI403HandlingState) {
	if ctx == nil {
		ctx = context.Background()
	}
	state := &openAI403HandlingState{}
	return context.WithValue(ctx, openAI403HandlingStateContextKey{}, state), state
}

func markOpenAI403ModelScoped(ctx context.Context) {
	if ctx == nil {
		return
	}
	if state, ok := ctx.Value(openAI403HandlingStateContextKey{}).(*openAI403HandlingState); ok && state != nil {
		state.modelScoped = true
	}
}
