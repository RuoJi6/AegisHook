export interface PromptTemplate {
  prompt: string;
  dataGuardPrompt: string;
  dataGuardStart: string;
  dataGuardEnd: string;
  dataGuardAnchor: string;
}

export function hasDataGuard(prompt: string, template: PromptTemplate) {
  const start = prompt.indexOf(template.dataGuardStart);
  return start >= 0 && prompt.indexOf(template.dataGuardEnd, start) >= 0;
}

export function setDataGuard(
  prompt: string,
  enabled: boolean,
  template: PromptTemplate,
) {
  if (enabled && hasDataGuard(prompt, template)) return prompt;
  if (enabled) {
    if (
      prompt.includes(template.dataGuardStart) ||
      prompt.includes(template.dataGuardEnd)
    )
      throw new Error("数据获取限制的起止标记不完整，请先修复提示词中的标记。");
    const anchor = prompt.indexOf(template.dataGuardAnchor);
    const index = anchor < 0 ? 0 : anchor;
    return (
      prompt.slice(0, index) +
      template.dataGuardPrompt +
      "\n\n" +
      prompt.slice(index)
    );
  }
  // Remove complete managed blocks only; never remove adjacent custom content.
  while (hasDataGuard(prompt, template)) {
    const start = prompt.indexOf(template.dataGuardStart);
    let end =
      prompt.indexOf(template.dataGuardEnd, start) +
      template.dataGuardEnd.length;
    if (prompt.slice(end, end + 2) === "\n\n") end += 2;
    prompt = prompt.slice(0, start) + prompt.slice(end);
  }
  return prompt;
}
