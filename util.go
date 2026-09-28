package governance

import (
	"sort"
	"strconv"
)

func itoa(v int64) string { return strconv.FormatInt(v, 10) }

// pct 把比例格式化为百分比字符串，便于在判定依据/日志中阅读。
func pct(v float64) string { return strconv.FormatFloat(v*100, 'f', 2, 64) + "%" }

// sortVotes 让选票返回顺序确定，便于测试断言与日志阅读。
func sortVotes(vs []*Vote) {
	sort.Slice(vs, func(i, j int) bool { return vs[i].Voter < vs[j].Voter })
}
