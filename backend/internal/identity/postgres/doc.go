// Package postgres 持久化用户、认证身份、待完成登录会话和分组授权。
//
// 阅读入口：
//   - user_repo.go：用户资料、邮箱查重、分组授权和余额操作。
//   - auth_state.go：注册和登录时的认证身份写入。
//   - pending.go：待完成登录会话、完成码和资料采纳选择。
//
// 文件分组：
//   - user_*.go：用户仓储、身份绑定、属性定义和实体转换。
//   - auth_*.go、oauth_lookup.go：注册登录的数据操作与 OAuth 身份查询。
//   - pending*.go：登录会话管理、身份绑定和资料采纳。
//   - admin_mutations.go、bootstrap_admin.go：管理员初始化及后台用户操作。
//   - group_access_*.go：在调用方事务中添加或删除分组授权。
//   - passkey.go：Passkey 凭据的保存、查询和更新。
//   - concurrency.go、risk_status.go：并发额度修改与风控禁用状态写入。
package postgres
