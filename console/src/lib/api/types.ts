// TypeScript type definitions derived from api/spec/openapi.json

export type WorkflowStatus =
  | "PENDING"
  | "SUCCESS"
  | "ERROR"
  | "CANCELLED"
  | "ENQUEUED"
  | "DELAYED"
  | "MAX_RECOVERY_ATTEMPTS_EXCEEDED";

export type ExecutorStatus = "HEALTHY" | "DISCONNECTED" | "DEAD";

export interface Application {
  id: string;
  name: string;
  orgId: string;
  status: "AVAILABLE" | "UNAVAILABLE";
  dbosCloud: boolean;
  privateMode: boolean;
  executorTimeoutSecs: number;
  gcRowsThreshold?: number | null;
  gcTimeThresholdMs?: number | null;
  globalTimeoutMs?: number | null;
  language?: string | null;
}

export interface Executor {
  executorId: string;
  appId: string;
  appVersion: string;
  status: ExecutorStatus;
  hostId?: string | null;
  hostname?: string | null;
  createdAt: string;
  updatedAt: string;
  language?: string | null;
  dbosVersion?: string | null;
  executorMetadata?: Record<string, unknown> | null;
}

export interface Workflow {
  workflowId: string;
  status: WorkflowStatus;
  workflowName?: string | null;
  workflowClass?: string | null;
  workflowConfig?: string | null;
  user?: string | null;
  assumedRole?: string | null;
  roles?: string | null;
  input?: string | null;
  output?: string | null;
  error?: string | null;
  createdAt: string;
  updatedAt: string;
  queueName?: string | null;
  appVersion?: string | null;
  executorId?: string | null;
  timeoutMs?: number | null;
  deadline?: string | null;
  deduplicationId?: string | null;
  priority: number;
  queuePartitionKey?: string | null;
  forkedFrom?: string | null;
  wasForkedFrom: boolean;
  parentWorkflowId?: string | null;
  dequeuedAt?: string | null;
  delayUntil?: string | null;
  completedAt?: string | null;
  attributes?: string | null;
  scheduleName?: string | null;
  applicationName?: string | null;
}

export interface Step {
  stepId: number;
  stepName: string;
  output?: string | null;
  error?: string | null;
  childWorkflowId?: string | null;
  startedAt?: string | null;
  completedAt?: string | null;
}

export interface Event {
  key: string;
  value: string;
}

export interface Notification {
  topic?: string | null;
  message: string;
  createdAt: string;
  consumed: boolean;
}

export interface StreamEntry {
  key: string;
  values: string[];
}

export interface Queue {
  name: string;
  concurrency?: number | null;
  workerConcurrency?: number | null;
  rateLimitMax?: number | null;
  rateLimitPeriodSecs?: number | null;
  priorityEnabled: boolean;
  partitionQueue: boolean;
  pollingIntervalSecs: number;
  applicationName?: string | null;
}

export interface Schedule {
  scheduleId: string;
  scheduleName: string;
  workflowName: string;
  workflowClass?: string | null;
  cronExpression: string;
  cronTimezone?: string | null;
  status: string;
  context?: string | null;
  lastFiredAt?: string | null;
  automaticBackfill: boolean;
  applicationName?: string | null;
}

export interface AlertingRule {
  id: string;
  appId: string;
  receivingAppId: string;
  ruleType: "WorkflowFailure" | "SlowQueue" | "UnresponsiveApplication" | string;
  ruleMetadata: Record<string, unknown>;
  minIntervalSecs?: number | null;
  lastFiredAt?: string | null;
}

export interface CreateAlertInput {
  ruleType: string;
  receivingAppName?: string;
  minIntervalSecs?: number;
  ruleMetadata: Record<string, unknown>;
}

export interface Token {
  tokenName: string;
  createdAt: string;
  permissions: string[];
  appIds: string[];
}

export interface TokenCreated {
  token: string;
  tokenName: string;
}

export type ApiKey = Token;

export interface WorkflowSearchQuery {
  workflowIds?: string[];
  workflowName?: string[];
  workflowIdPrefix?: string[];
  status?: string[];
  queueName?: string[];
  appVersion?: string[];
  executorId?: string[];
  startTime?: string;
  endTime?: string;
  limit?: number;
  offset?: number;
  sortDesc?: boolean;
}
