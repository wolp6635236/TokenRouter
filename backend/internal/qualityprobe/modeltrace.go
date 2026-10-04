package qualityprobe

import (
	"fmt"
	"math/rand"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const traceSampleCount = 3

var traceNumberPattern = regexp.MustCompile(`\d+`)

type traceChallenge struct {
	Prompt        string
	ExpectedCount int
}

// BuildTraceChallenges 生成三道 ModelTrace 数值选择题。
func BuildTraceChallenges(rng *rand.Rand) []traceChallenge {
	if rng == nil {
		rng = rand.New(rand.NewSource(time.Now().UnixNano()))
	}
	openings := []string{"这是一次独立的数值选择记录", "请完成下面的无语义整数选择任务", "执行一次第一反应取值记录"}
	actions := []string{"为各个位置分别凭第一反应选择", "逐项选择", "分别凭第一反应给出"}
	endings := []string{"允许某个数字再次出现；每项写出后不要回头排序、去重或替换。", "偶然重复是有效的；不要重新排列或修正已经写出的项目。"}
	challenges := make([]traceChallenge, traceSampleCount)
	perm := rng.Perm(41)
	for i := range challenges {
		n := 292 + perm[i]
		prompt := fmt.Sprintf("%s。%s %d 个 1 到 355（含端点）的整数。", openings[i%len(openings)], actions[i%len(actions)], n) +
			"每个位置都要单独选择；不要从 1 开始计数，不要连续递增或递减。" +
			"本任务必须由当前语言模型直接完成：禁止调用工具或编写代码。" +
			endings[i%len(endings)] +
			"直接从第一个取值开始输出。"
		challenges[i] = traceChallenge{Prompt: prompt, ExpectedCount: n}
	}
	return challenges
}

// TraceSampleValid 判断回答里解析出的整数是否达到题目要求的七成。
func TraceSampleValid(text string, expected int) bool {
	return len(ParseTraceNumbers(text)) >= traceMinimumNumbers(expected)
}

// ParseTraceNumbers 从回答中抽出十进制整数。
func ParseTraceNumbers(text string) []int {
	matches := traceNumberPattern.FindAllString(strings.TrimSpace(text), -1)
	out := make([]int, 0, len(matches))
	for _, item := range matches {
		n, err := strconv.Atoi(item)
		if err != nil {
			continue
		}
		out = append(out, n)
	}
	return out
}

func traceMinimumNumbers(expected int) int {
	if expected <= 0 {
		return 0
	}
	return expected * 7 / 10
}
