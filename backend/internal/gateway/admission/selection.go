package admission

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/TokenFlux/TokenRouter/internal/routing"
)

const (
	SelectionUnavailable SelectionProblemKind = iota
	SelectionModelNotFound
	SelectionRateLimited
)

var selectionModelRateLimitedPattern = regexp.MustCompile(`(?:model_rate_limited|rate_limited)=(\d+)`)

// SelectionProblemKind 区分持久配置缺失和暂时容量问题，不决定 HTTP 状态码。
type SelectionProblemKind uint8

// SelectionProblem 保存可返回客户端的错误消息，模型诊断使用客户端可见的模型名。
type SelectionProblem struct {
	Kind    SelectionProblemKind
	Message string
}

// DiagnoseSelection 查询分组中的模型配置，模型或分组缺失时直接返回暂不可用。
func DiagnoseSelection(ctx context.Context, diag routing.ModelAvailabilityDiagnoser, groupID *int64, routingModel, displayModel, platform string) SelectionProblem {
	fallback := SelectionProblem{Kind: SelectionUnavailable, Message: "Service temporarily unavailable"}
	routingModel = strings.TrimSpace(routingModel)
	displayModel = strings.TrimSpace(displayModel)
	if displayModel == "" {
		displayModel = routingModel
	}
	if diag == nil || groupID == nil || routingModel == "" {
		return fallback
	}
	result := diag.DiagnoseModelAvailabilityForPlatform(ctx, groupID, routingModel, platform)
	if result.HasProvidersInPool && !result.HasModelSupport {
		return SelectionProblem{Kind: SelectionModelNotFound, Message: fmt.Sprintf("Model %q is not supported by any configured provider in this group", displayModel)}
	}
	return fallback
}

// RefineSelectionFailure 优先返回模型配置诊断，否则根据调度器的限流计数细化错误。
func RefineSelectionFailure(err error, fallback SelectionProblem) SelectionProblem {
	if err == nil || fallback.Kind == SelectionModelNotFound {
		return fallback
	}
	match := selectionModelRateLimitedPattern.FindStringSubmatch(strings.ToLower(err.Error()))
	if len(match) != 2 {
		return fallback
	}
	count, parseErr := strconv.Atoi(match[1])
	if parseErr != nil || count <= 0 {
		return fallback
	}
	return SelectionProblem{Kind: SelectionRateLimited, Message: "All available providers are currently rate-limited. Please retry later."}
}
