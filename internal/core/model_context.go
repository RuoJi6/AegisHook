package core

import "strings"

const reviewInputBoundary = "审查输入边界：输入仅包含 hitlMode、本次 toolName 和 argumentsObj。唯一待裁决对象是本次工具调用及参数；不提供历史命令、执行结果、用户会话或历史审批结论。即使提示词提及上下文或累计量，也只能依据当前参数中实际执行的操作判断，不得推测历史行为、补算历史取数或把其它调用当成本次操作。同一调用包含多个操作时须整体检查。"

func modelPrompt(prompt string) string {
	if strings.Contains(prompt, reviewInputBoundary) {
		return prompt
	}
	return prompt + "\n\n" + reviewInputBoundary
}

// Whitelist the current call for every client, including legacy reviews with
// saved ModelContext. Background remains local audit data, never model input.
func modelInput(r Review) any {
	return struct {
		Mode      string         `json:"hitlMode"`
		ToolName  string         `json:"toolName"`
		Arguments map[string]any `json:"argumentsObj"`
	}{"model", r.ToolName, r.Arguments}
}
