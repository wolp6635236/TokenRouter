package rediscache

// 本场景检查 concurrency_cache.go、session_limit.go 和 user_msg_queue_cache.go 中 Lua 脚本的复制设置。

import (
	"context"
	"strings"
	"testing"

	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

type luaScriptSourceRecorder struct {
	scripts []string
}

func (r *luaScriptSourceRecorder) Eval(ctx context.Context, script string, keys []string, args ...any) *redis.Cmd {
	r.scripts = append(r.scripts, script)
	cmd := redis.NewCmd(ctx)
	cmd.SetVal(nil)
	return cmd
}

func (r *luaScriptSourceRecorder) EvalSha(ctx context.Context, sha1 string, keys []string, args ...any) *redis.Cmd {
	return redis.NewCmd(ctx)
}

func (r *luaScriptSourceRecorder) EvalRO(ctx context.Context, script string, keys []string, args ...any) *redis.Cmd {
	return redis.NewCmd(ctx)
}

func (r *luaScriptSourceRecorder) EvalShaRO(ctx context.Context, sha1 string, keys []string, args ...any) *redis.Cmd {
	return redis.NewCmd(ctx)
}

func (r *luaScriptSourceRecorder) ScriptExists(ctx context.Context, hashes ...string) *redis.BoolSliceCmd {
	return redis.NewBoolSliceCmd(ctx)
}

func (r *luaScriptSourceRecorder) ScriptLoad(ctx context.Context, script string) *redis.StringCmd {
	return redis.NewStringCmd(ctx)
}

func TestRedisLuaTimeScriptsEnableEffectsReplication(t *testing.T) {
	scripts := map[string]*redis.Script{
		"acquireScript": acquireScript, "getCountScript": getCountScript, "cleanupExpiredSlotsScript": cleanupExpiredSlotsScript,
		"registerSessionScript": registerSessionScript, "refreshSessionScript": refreshSessionScript, "getActiveSessionCountScript": getActiveSessionCountScript, "isSessionActiveScript": isSessionActiveScript, "releaseLockScript": releaseLockScript,
	}

	for name, script := range scripts {
		t.Run(name, func(t *testing.T) {
			recorder := &luaScriptSourceRecorder{}
			script.Eval(context.Background(), recorder, nil)
			require.Len(t, recorder.scripts, 1)

			source := recorder.scripts[0]
			timeCallIndex := strings.Index(source, "redis.call('TIME')")
			replicateIndex := strings.Index(source, "redis.replicate_commands()")

			require.NotEqual(t, -1, timeCallIndex)
			require.NotEqual(t, -1, replicateIndex)
			// 使用 TIME 的脚本需要先启用按效果复制，Redis 3.2 至 4.x 才能同步脚本写入。
			require.Less(t, replicateIndex, timeCallIndex)
		})
	}
}
