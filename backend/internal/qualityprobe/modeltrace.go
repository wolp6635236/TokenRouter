package qualityprobe

import (
	"fmt"
	"math"
	"math/rand"
	"regexp"
	"time"
)

const traceSampleCount = 1

var traceNumberPattern = regexp.MustCompile(`\d+`)

type traceChallenge struct {
	Prompt        string
	ExpectedCount int
}

// BuildTraceChallenges 生成一道 ModelTrace 数值选择题。
func BuildTraceChallenges(rng *rand.Rand) []traceChallenge {
	if rng == nil {
		rng = rand.New(rand.NewSource(time.Now().UnixNano()))
	}
	openings := []string{
		"这是一次独立的数值选择记录",
		"请完成下面的无语义整数选择任务",
		"执行一次第一反应取值记录",
	}
	actions := []string{
		"为各个位置分别凭第一反应选择",
		"逐项选择",
		"分别凭第一反应给出",
	}
	endings := []string{
		"允许某个数字再次出现；每项写出后不要回头排序、去重或替换。",
		"偶然重复是有效的；不要重新排列或修正已经写出的项目。",
		"相同值可以再次出现；输出过程中不要整理或改写前面的项目。",
	}
	challenges := make([]traceChallenge, traceSampleCount)
	perm := rng.Perm(41)
	for i := range challenges {
		n := 292 + perm[i]
		prompt := fmt.Sprintf("%s。%s %d 个 1 到 355（含端点）的整数。", openings[i%len(openings)], actions[i%len(actions)], n) +
			"每个位置都要单独选择；不要从 1 开始计数，不要连续递增或递减，也不要采用等差、循环、重复区块或其他规则化模式。" +
			"本任务必须由当前语言模型直接完成：禁止调用工具或编写代码。" +
			endings[i%len(endings)] +
			"直接从第一个取值开始输出。"
		challenges[i] = traceChallenge{Prompt: prompt, ExpectedCount: n}
	}
	return challenges
}

// TraceSampleValid 判断该回答能否进入指纹库归因：有效整数不少于 80，且达到题目数量的 55%。
func TraceSampleValid(text string, expected int) bool {
	return len(parseFingerprintNumbers(text)) >= traceMinimumNumbers(expected)
}

// ParseTraceNumbers 从回答中抽出 1 到 355 的最长连续整数段。
func ParseTraceNumbers(text string) []int {
	return parseFingerprintNumbers(text)
}

func traceMinimumNumbers(expected int) int {
	minimum := 80
	if expected > 0 {
		needed := int(math.Ceil(float64(expected) * 0.55))
		if needed > minimum {
			minimum = needed
		}
	}
	return minimum
}
