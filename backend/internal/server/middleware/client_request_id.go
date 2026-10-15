package middleware

import (
	"context"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/Wei-Shaw/sub2api/internal/pkg/usagesource"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"go.uber.org/zap"
)

const clientRequestIDHeader = "X-Client-Request-ID"

// ClientRequestID ensures every request has a unique client_request_id in request.Context().
//
// This is used by the Ops monitoring module for end-to-end request correlation.
func ClientRequestID() gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.Request == nil {
			c.Next()
			return
		}

		ctx := c.Request.Context()
		if v, _ := c.Request.Context().Value(ctxkey.ClientRequestID).(string); strings.TrimSpace(v) != "" {
			var valid bool
			v, valid = normalizeCorrelationID(v)
			if !valid {
				v = uuid.New().String()
			}
			c.Header(clientRequestIDHeader, v)
			ctx = context.WithValue(ctx, ctxkey.ClientRequestID, v)
			c.Request = c.Request.WithContext(withUsageSourceFromHeader(ctx, c))
			c.Next()
			return
		}

		id := uuid.New().String()
		c.Header(clientRequestIDHeader, id)
		ctx = context.WithValue(ctx, ctxkey.ClientRequestID, id)
		requestLogger := logger.FromContext(ctx).With(zap.String("client_request_id", strings.TrimSpace(id)))
		ctx = logger.IntoContext(ctx, requestLogger)
		c.Request = c.Request.WithContext(withUsageSourceFromHeader(ctx, c))
		c.Next()
	}
}

func withUsageSourceFromHeader(ctx context.Context, c *gin.Context) context.Context {
	if c == nil || c.Request == nil {
		return ctx
	}
	if source, ok := usagesource.NormalizeVerified(c.GetHeader(usagesource.Header), c.GetHeader(usagesource.SignatureHeader)); ok {
		return context.WithValue(ctx, ctxkey.UsageSource, source)
	}
	return ctx
}
