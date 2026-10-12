// Package upstream 定义上游请求执行和响应输出接口，并提供各平台共用的报文处理函数。
//
// 阅读入口：
//   - attempt.go：定义单次请求的输入、结果和执行接口。
//   - output.go：把响应头、字节和刷新操作传给调用方的输出适配器。
//   - image_request_parse.go：解析图片生成和编辑请求。
//
// 文件分组：
//   - attempt.go、output.go、frame.go 和 responses_observation.go 定义请求执行、输出和观察值。
//   - image*.go 处理图片模型、请求字段、上传内容和返回字节。
//   - error_message.go、model_error.go 和 stream_error.go 识别错误并整理客户端错误描述。
//   - client_tools_stream.go 转换 Responses 流中的客户端工具事件。
//   - http_profile.go、path_segment.go、session_identity.go 和 wait.go 处理传输策略、路径校验、会话标识和可取消等待。
package upstream
