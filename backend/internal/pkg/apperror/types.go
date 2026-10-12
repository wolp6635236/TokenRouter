package apperror

const (
	// CategoryOK 保留对应错误类别的稳定标识。
	CategoryOK Category = 200
	// CategoryBadRequest 保留对应错误类别的稳定标识。
	CategoryBadRequest Category = 400
	// CategoryUnauthorized 保留对应错误类别的稳定标识。
	CategoryUnauthorized Category = 401
	// CategoryForbidden 保留对应错误类别的稳定标识。
	CategoryForbidden Category = 403
	// CategoryNotFound 保留对应错误类别的稳定标识。
	CategoryNotFound Category = 404
	// CategoryConflict 保留对应错误类别的稳定标识。
	CategoryConflict Category = 409
	// CategoryTooManyRequests 保留对应错误类别的稳定标识。
	CategoryTooManyRequests Category = 429
	// CategoryClientClosed 保留对应错误类别的稳定标识。
	CategoryClientClosed Category = 499
	// CategoryInternalServer 保留对应错误类别的稳定标识。
	CategoryInternalServer Category = 500
	// CategoryBadGateway 保留对应错误类别的稳定标识。
	CategoryBadGateway Category = 502
	// CategoryServiceUnavailable 保留对应错误类别的稳定标识。
	CategoryServiceUnavailable Category = 503
	// CategoryGatewayTimeout 保留对应错误类别的稳定标识。
	CategoryGatewayTimeout Category = 504
)

// Category 为应用错误类别；数值在兼容期保留旧错误身份，不依赖 HTTP 包。
type Category int32

// BadRequest new BadRequest error that is mapped to a 400 response.
func BadRequest(reason, message string) *ApplicationError {
	return New(CategoryBadRequest, reason, message)
}

// IsBadRequest determines if err is an error which indicates a BadRequest error.
// It supports wrapped errors.
func IsBadRequest(err error) bool {
	return CategoryOf(err) == CategoryBadRequest
}

// TooManyRequests new TooManyRequests error that is mapped to a 429 response.
func TooManyRequests(reason, message string) *ApplicationError {
	return New(CategoryTooManyRequests, reason, message)
}

// Unauthorized new Unauthorized error that is mapped to a 401 response.
func Unauthorized(reason, message string) *ApplicationError {
	return New(CategoryUnauthorized, reason, message)
}

// Forbidden new Forbidden error that is mapped to a 403 response.
func Forbidden(reason, message string) *ApplicationError {
	return New(CategoryForbidden, reason, message)
}

// NotFound new NotFound error that is mapped to a 404 response.
func NotFound(reason, message string) *ApplicationError {
	return New(CategoryNotFound, reason, message)
}

// IsNotFound determines if err is an error which indicates an NotFound error.
// It supports wrapped errors.
func IsNotFound(err error) bool {
	return CategoryOf(err) == CategoryNotFound
}

// Conflict new Conflict error that is mapped to a 409 response.
func Conflict(reason, message string) *ApplicationError {
	return New(CategoryConflict, reason, message)
}

// InternalServer new InternalServer error that is mapped to a 500 response.
func InternalServer(reason, message string) *ApplicationError {
	return New(CategoryInternalServer, reason, message)
}

// ServiceUnavailable new ServiceUnavailable error that is mapped to an HTTP 503 response.
func ServiceUnavailable(reason, message string) *ApplicationError {
	return New(CategoryServiceUnavailable, reason, message)
}

// GatewayTimeout new GatewayTimeout error that is mapped to an HTTP 504 response.
func GatewayTimeout(reason, message string) *ApplicationError {
	return New(CategoryGatewayTimeout, reason, message)
}
