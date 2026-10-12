package apikey

import (
	"context"
)

// RequestMetadata 保存网关入口信息，并用 Set 字段区分未设置和空值。
type RequestMetadata struct {
	ForcePlatform      string
	ForcePlatformSet   bool
	InboundEndpoint    string
	InboundEndpointSet bool
}

type requestMetadataKey struct{}

func WithRequestMetadata(ctx context.Context, value RequestMetadata) context.Context {
	return context.WithValue(ctx, requestMetadataKey{}, value)
}

// WithForcePlatform 在上下文中设置平台，同时标记该字段已设置。
func WithForcePlatform(ctx context.Context, platform string) context.Context {
	value := RequestMetadataFromContext(ctx)
	value.ForcePlatform, value.ForcePlatformSet = platform, true
	return WithRequestMetadata(ctx, value)
}

// WithInboundEndpoint 将调用方已规范化的入口保存到 context。
func WithInboundEndpoint(ctx context.Context, endpoint string) context.Context {
	value := RequestMetadataFromContext(ctx)
	value.InboundEndpoint, value.InboundEndpointSet = endpoint, true
	return WithRequestMetadata(ctx, value)
}

func RequestMetadataFromContext(ctx context.Context) RequestMetadata {
	if ctx == nil {
		return RequestMetadata{}
	}
	v, _ := ctx.Value(requestMetadataKey{}).(RequestMetadata)
	return v
}

func ForcePlatformFromContext(ctx context.Context) (string, bool) {
	v := RequestMetadataFromContext(ctx)
	return v.ForcePlatform, v.ForcePlatformSet
}
