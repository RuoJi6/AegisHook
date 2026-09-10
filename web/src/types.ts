export interface Call {
  agent?: string;
  id: string;
  instanceId: string;
  sessionId: string;
  callId: string;
  toolName: string;
  argumentsObj: Record<string, unknown>;
  userMessage?: string;
  context?: string;
  cwd: string;
  digest: string;
  mode: string;
  reviewPath?: "scope" | "rule" | "human" | "model";
  rules?: Rule[];
  needsHuman?: boolean;
  modelVerdict?: { decision: string; comment: string; ruleId: string };
  version: number;
  prompt: string;
  decision: string;
  ruleId: string;
  comment: string;
  execution: string;
  result?: string;
  createdAt: string;
  decidedAt: string | null;
  deadline: string;
}
export interface Instance {
  nodeId?: string;
  platform?: string;
  disconnectReason?: string;
  agent?: string;
  agentVersion?: string;
  connectionMode?: string;
  id: string;
  sessionId: string;
  cwd: string;
  piVersion: string;
  hookVersion: string;
  installations: string[];
  heartbeat: string;
  state: string;
  online: boolean;
}
export interface Installation {
  agent?: string;
  id: string;
  scope: string;
  project: string;
  entry: string;
  installed: boolean;
  status: string;
  configStatus: "configured" | "entry_error" | "uninstalled";
  observed: boolean;
  version: string;
}
export interface RuleSemantics {
  summary: string;
  checks: string[];
  examples: { tool: string; argumentsObj: Record<string, unknown> }[];
  limits: string;
}
export interface Rule {
  semantics?: RuleSemantics;
  id: string;
  name: string;
  decision: string;
  enabled: boolean;
  builtin: boolean;
  tool: string;
  field: string;
  pattern: string;
  matcher: "semantic" | "regex" | "contains";
  target: "arguments" | "toolName";
  priority: number;
  message: string;
}
export interface Scope {
  nodeId?: string;
  platform?: string;
  id: string;
  name: string;
  project: string;
  targets: string[];
  paths: string[];
}
export interface ClientNode {
  id: string;
  name: string;
  platform: string;
  createdAt: string;
  revoked: boolean;
}
export interface ClientRequest {
  id: string;
  name: string;
  platform: string;
  ip: string;
  state: string;
  createdAt: string;
  expiresAt: string;
}
export interface ClientIPBlock {
  id: string;
  ip: string;
  createdAt: string;
}
export interface Settings {
  mode: string;
  version: number;
  approvalSeconds: number;
  modelSeconds: number;
  prompt: string;
  model: {
    pricing: ModelPricing;
    protocol: string;
    baseUrl: string;
    model: string;
    hasKey: boolean;
    tested: boolean;
  };
}

export interface AgentInfo {
  id: string;
  name: string;
  found: boolean;
  version: string;
  path: string;
  platform: string;
  integration: string;
  instructions: string;
  limitations: string;
}

export interface ModelPricing {
  enabled: boolean;
  currency: string;
  input: number;
  output: number;
  cacheRead: number;
  cacheWrite: number;
}
export interface TokenUsage {
  input: number;
  output: number;
  cacheRead: number;
  cacheWrite: number;
}
export interface UsageRecord {
  id: string;
  reviewId?: string;
  purpose: string;
  model: string;
  protocol: string;
  version: number;
  createdAt: string;
  usage: TokenUsage | null;
  pricing: ModelPricing;
  cost: number | null;
  failed: boolean;
}
