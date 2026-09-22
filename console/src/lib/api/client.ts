// Relay Conductor v2 API Client

import type {
  Application,
  Executor,
  Workflow,
  Step,
  Event,
  Notification,
  StreamEntry,
  Queue,
  Schedule,
  AlertingRule,
  CreateAlertInput,
  Token,
  TokenCreated,
  ApiKey,
  WorkflowSearchQuery,
  UpdateAppInput,
  AutoscalePolicy,
  QueueAutoscale,
  AuditLogEntry,
  AuditLogQuery,
  MetricsQuery,
  OrgMembers,
  Role,
} from "./types.js";

export class ApiClient {
  private baseUrl: string;
  private apiKey: string | null;

  constructor(baseUrl: string = "", apiKey: string | null = null) {
    this.baseUrl = baseUrl.replace(/\/+$/, "");
    this.apiKey = apiKey;
  }

  setApiKey(key: string | null): void {
    this.apiKey = key;
  }

  private async request<T>(path: string, options: RequestInit = {}): Promise<T> {
    const headers = new Headers(options.headers || {});
    headers.set("Accept", "application/json");

    if (options.body && typeof options.body === "string") {
      headers.set("Content-Type", "application/json");
    }

    if (this.apiKey) {
      headers.set("Authorization", `Bearer ${this.apiKey}`);
    }

    const url = `${this.baseUrl}${path}`;
    const response = await fetch(url, { ...options, headers });

    if (!response.ok) {
      let errorDetail = `HTTP ${response.status} ${response.statusText}`;
      try {
        const errorJson = await response.json();
        if (errorJson.detail) {
          errorDetail = errorJson.detail;
        } else if (errorJson.title) {
          errorDetail = errorJson.title;
        } else if (errorJson.message) {
          errorDetail = errorJson.message;
        }
      } catch {
        // Non-json response
      }
      const err = new Error(errorDetail);
      (err as unknown as { status: number }).status = response.status;
      throw err;
    }

    if (response.status === 204) {
      return undefined as unknown as T;
    }

    return response.json() as Promise<T>;
  }

  // System & Health
  async getHealth(): Promise<{ status: string }> {
    return this.request<{ status: string }>("/healthz");
  }

  // Applications
  async listApplications(orgName: string = "default"): Promise<Application[]> {
    return this.request<Application[]>(`/v2/orgs/${encodeURIComponent(orgName)}/apps`);
  }

  async getApplication(orgName: string, appName: string): Promise<Application> {
    return this.request<Application>(
      `/v2/orgs/${encodeURIComponent(orgName)}/apps/${encodeURIComponent(appName)}`
    );
  }

  async listExecutors(orgName: string, appName: string): Promise<Executor[]> {
    return this.request<Executor[]>(
      `/v2/orgs/${encodeURIComponent(orgName)}/apps/${encodeURIComponent(appName)}/executors`
    );
  }

  // Workflows
  async listWorkflows(
    orgName: string,
    appName: string,
    query: WorkflowSearchQuery = {}
  ): Promise<Workflow[]> {
    const params = new URLSearchParams();
    if (query.workflowIds && query.workflowIds.length > 0) {
      for (const id of query.workflowIds) params.append("workflowIds", id);
    }
    if (query.workflowName && query.workflowName.length > 0) {
      for (const name of query.workflowName) params.append("workflowName", name);
    }
    if (query.status && query.status.length > 0) {
      for (const s of query.status) params.append("status", s);
    }
    if (query.queueName && query.queueName.length > 0) {
      for (const q of query.queueName) params.append("queueName", q);
    }
    if (query.appVersion && query.appVersion.length > 0) {
      for (const v of query.appVersion) params.append("appVersion", v);
    }
    if (query.limit) params.append("limit", query.limit.toString());
    if (query.offset) params.append("offset", query.offset.toString());
    if (query.sortDesc !== undefined) params.append("sortDesc", query.sortDesc ? "true" : "false");

    const qs = params.toString();
    const path = `/v2/orgs/${encodeURIComponent(orgName)}/apps/${encodeURIComponent(appName)}/workflows${qs ? "?" + qs : ""}`;
    return this.request<Workflow[]>(path);
  }

  async getWorkflow(orgName: string, appName: string, workflowId: string): Promise<Workflow> {
    return this.request<Workflow>(
      `/v2/orgs/${encodeURIComponent(orgName)}/apps/${encodeURIComponent(appName)}/workflows/${encodeURIComponent(workflowId)}`
    );
  }

  async listSteps(orgName: string, appName: string, workflowId: string): Promise<Step[]> {
    return this.request<Step[]>(
      `/v2/orgs/${encodeURIComponent(orgName)}/apps/${encodeURIComponent(appName)}/workflows/${encodeURIComponent(workflowId)}/steps`
    );
  }

  async getWorkflowEvents(orgName: string, appName: string, workflowId: string): Promise<Event[]> {
    return this.request<Event[]>(
      `/v2/orgs/${encodeURIComponent(orgName)}/apps/${encodeURIComponent(appName)}/workflows/${encodeURIComponent(workflowId)}/events`
    );
  }

  async getWorkflowNotifications(
    orgName: string,
    appName: string,
    workflowId: string
  ): Promise<Notification[]> {
    return this.request<Notification[]>(
      `/v2/orgs/${encodeURIComponent(orgName)}/apps/${encodeURIComponent(appName)}/workflows/${encodeURIComponent(workflowId)}/notifications`
    );
  }

  async getWorkflowStreams(
    orgName: string,
    appName: string,
    workflowId: string,
    key?: string
  ): Promise<StreamEntry[]> {
    const qs = key ? `?key=${encodeURIComponent(key)}` : "";
    return this.request<StreamEntry[]>(
      `/v2/orgs/${encodeURIComponent(orgName)}/apps/${encodeURIComponent(appName)}/workflows/${encodeURIComponent(workflowId)}/streams${qs}`
    );
  }

  async cancelWorkflow(orgName: string, appName: string, workflowId: string): Promise<void> {
    await this.request(
      `/v2/orgs/${encodeURIComponent(orgName)}/apps/${encodeURIComponent(appName)}/workflows/${encodeURIComponent(workflowId)}/cancel`,
      { method: "POST", body: "{}" }
    );
  }

  async resumeWorkflow(orgName: string, appName: string, workflowId: string): Promise<void> {
    await this.request(
      `/v2/orgs/${encodeURIComponent(orgName)}/apps/${encodeURIComponent(appName)}/workflows/${encodeURIComponent(workflowId)}/resume`,
      { method: "POST", body: "{}" }
    );
  }


  async forkWorkflow(
    org: string,
    app: string,
    id: string,
    startStep: number
  ): Promise<{ workflowId: string }> {
    return this.request<{ workflowId: string }>(
      `/v2/orgs/${encodeURIComponent(org)}/apps/${encodeURIComponent(app)}/workflows/${encodeURIComponent(id)}/fork`,
      {
        method: "POST",
        body: JSON.stringify({ startStep }),
      }
    );
  }

  // Queues
  async listQueues(orgName: string, appName: string): Promise<Queue[]> {
    return this.request<Queue[]>(
      `/v2/orgs/${encodeURIComponent(orgName)}/apps/${encodeURIComponent(appName)}/queues`
    );
  }

  // Schedules
  async listSchedules(orgName: string, appName: string): Promise<Schedule[]> {
    return this.request<Schedule[]>(
      `/v2/orgs/${encodeURIComponent(orgName)}/apps/${encodeURIComponent(appName)}/schedules`
    );
  }

  async pauseSchedule(orgName: string, appName: string, scheduleName: string): Promise<void> {
    await this.request(
      `/v2/orgs/${encodeURIComponent(orgName)}/apps/${encodeURIComponent(appName)}/schedules/${encodeURIComponent(scheduleName)}/pause`,
      { method: "POST" }
    );
  }

  async resumeSchedule(orgName: string, appName: string, scheduleName: string): Promise<void> {
    await this.request(
      `/v2/orgs/${encodeURIComponent(orgName)}/apps/${encodeURIComponent(appName)}/schedules/${encodeURIComponent(scheduleName)}/resume`,
      { method: "POST" }
    );
  }

  async triggerSchedule(
    orgName: string,
    appName: string,
    scheduleName: string
  ): Promise<{ workflowId: string }> {
    return this.request<{ workflowId: string }>(
      `/v2/orgs/${encodeURIComponent(orgName)}/apps/${encodeURIComponent(appName)}/schedules/${encodeURIComponent(scheduleName)}/trigger`,
      { method: "POST" }
    );
  }

  // Alerting Rules
  async listAlertingRules(orgName: string, appName: string): Promise<AlertingRule[]> {
    return this.request<AlertingRule[]>(
      `/v2/orgs/${encodeURIComponent(orgName)}/apps/${encodeURIComponent(appName)}/alerting-rules`
    );
  }

  async createAlertingRule(
    orgName: string,
    appName: string,
    rule: CreateAlertInput
  ): Promise<AlertingRule> {
    return this.request<AlertingRule>(
      `/v2/orgs/${encodeURIComponent(orgName)}/apps/${encodeURIComponent(appName)}/alerting-rules`,
      {
        method: "POST",
        body: JSON.stringify(rule),
      }
    );
  }

  async deleteAlertingRule(orgName: string, appName: string, ruleId: string): Promise<void> {
    await this.request(
      `/v2/orgs/${encodeURIComponent(orgName)}/apps/${encodeURIComponent(appName)}/alerting-rules/${encodeURIComponent(ruleId)}`,
      { method: "DELETE" }
    );
  }

  // API Keys (Tokens)
  async listAPIKeys(orgName: string): Promise<Token[]> {
    return this.request<Token[]>(`/v2/orgs/${encodeURIComponent(orgName)}/tokens`);
  }

  async createAPIKey(
    orgName: string,
    name: string,
    permissions: string[] = ["*"],
    appNames: string[] = []
  ): Promise<TokenCreated> {
    return this.request<TokenCreated>(
      `/v2/orgs/${encodeURIComponent(orgName)}/tokens/${encodeURIComponent(name)}`,
      {
        method: "POST",
        body: JSON.stringify({ permissions, appNames }),
      }
    );
  }

  async revokeAPIKey(orgName: string, name: string): Promise<void> {
    await this.request(
      `/v2/orgs/${encodeURIComponent(orgName)}/tokens/${encodeURIComponent(name)}`,
      { method: "DELETE" }
    );
  }

  // Application settings (retention, timeouts, private mode)
  async updateApp(orgName: string, appName: string, input: UpdateAppInput): Promise<void> {
    await this.request(
      `/v2/orgs/${encodeURIComponent(orgName)}/apps/${encodeURIComponent(appName)}`,
      {
        method: "PATCH",
        body: JSON.stringify(input),
      }
    );
  }

  // Autoscaling policies and recommendations
  async getAutoscalingPolicy(orgName: string, appName: string): Promise<{ policy: AutoscalePolicy }> {
    return this.request<{ policy: AutoscalePolicy }>(
      `/v2/orgs/${encodeURIComponent(orgName)}/apps/${encodeURIComponent(appName)}/autoscaling-policy`
    );
  }

  async setAutoscalingPolicy(
    orgName: string,
    appName: string,
    policy: AutoscalePolicy
  ): Promise<{ policy: AutoscalePolicy }> {
    return this.request<{ policy: AutoscalePolicy }>(
      `/v2/orgs/${encodeURIComponent(orgName)}/apps/${encodeURIComponent(appName)}/autoscaling-policy`,
      {
        method: "PUT",
        body: JSON.stringify(policy),
      }
    );
  }

  async deleteAutoscalingPolicy(orgName: string, appName: string): Promise<void> {
    await this.request(
      `/v2/orgs/${encodeURIComponent(orgName)}/apps/${encodeURIComponent(appName)}/autoscaling-policy`,
      { method: "DELETE" }
    );
  }

  async getAutoscale(orgName: string, appName: string): Promise<QueueAutoscale[]> {
    return this.request<QueueAutoscale[]>(
      `/v2/orgs/${encodeURIComponent(orgName)}/apps/${encodeURIComponent(appName)}/autoscale`
    );
  }

  // Audit log
  async listAuditLogs(orgName: string, query: AuditLogQuery = {}): Promise<AuditLogEntry[]> {
    const params = new URLSearchParams();
    if (query.startTime) params.set("startTime", query.startTime);
    if (query.endTime) params.set("endTime", query.endTime);
    if (query.operation) params.set("operation", query.operation);
    if (query.subject) params.set("subject", query.subject);
    if (query.target) params.set("target", query.target);
    if (query.limit !== undefined) params.set("limit", String(query.limit));
    if (query.offset !== undefined) params.set("offset", String(query.offset));
    const qs = params.toString();
    return this.request<AuditLogEntry[]>(
      `/v2/orgs/${encodeURIComponent(orgName)}/audit-logs${qs ? `?${qs}` : ""}`
    );
  }

  // Metrics scrape (Prometheus text exposition, not JSON)
  async getMetricsText(query: MetricsQuery = {}): Promise<string> {
    const params = new URLSearchParams();
    if (query.applications) params.set("applications", query.applications);
    if (query.workflowNames) params.set("workflow_names", query.workflowNames);
    for (const m of query.metrics || []) params.append("metrics", m);
    const qs = params.toString();
    const url = `${this.baseUrl}/v1/metrics${qs ? `?${qs}` : ""}`;
    const headers = new Headers();
    headers.set("Accept", "text/plain");
    if (this.apiKey) {
      headers.set("Authorization", `Bearer ${this.apiKey}`);
    }
    const response = await fetch(url, { headers });
    if (!response.ok) {
      const err = new Error(`HTTP ${response.status} ${response.statusText}`);
      (err as unknown as { status: number }).status = response.status;
      throw err;
    }
    return response.text();
  }

  // Roles & Members
  async listRoles(orgName: string): Promise<Role[]> {
    return this.request<Role[]>(`/v2/orgs/${encodeURIComponent(orgName)}/roles`);
  }

  async createRole(orgName: string, name: string, permissions: string[]): Promise<{ name: string; permissions: string[] }> {
    return this.request<{ name: string; permissions: string[] }>(
      `/v2/orgs/${encodeURIComponent(orgName)}/roles`,
      { method: "POST", body: JSON.stringify({ name, permissions }) }
    );
  }

  async deleteRole(orgName: string, roleName: string): Promise<void> {
    await this.request<void>(
      `/v2/orgs/${encodeURIComponent(orgName)}/roles/${encodeURIComponent(roleName)}`,
      { method: "DELETE" }
    );
  }

  async listMembers(orgName: string): Promise<OrgMembers> {
    return this.request<OrgMembers>(`/v2/orgs/${encodeURIComponent(orgName)}/members`);
  }

  async grantMemberRole(orgName: string, username: string, roleName: string): Promise<void> {
    await this.request<void>(
      `/v2/orgs/${encodeURIComponent(orgName)}/members/${encodeURIComponent(username)}/roles/${encodeURIComponent(roleName)}`,
      { method: "PUT" }
    );
  }

  async removeMember(orgName: string, username: string): Promise<void> {
    await this.request<void>(
      `/v2/orgs/${encodeURIComponent(orgName)}/members/${encodeURIComponent(username)}`,
      { method: "DELETE" }
    );
  }

  async listPermissions(orgName: string): Promise<string[]> {
    return this.request<string[]>(`/v2/orgs/${encodeURIComponent(orgName)}/permissions`);
  }

  // Organization directory (Relay-native; absent on older servers).
  async listOrganizations(): Promise<string[]> {
    return this.request<string[]>(`/v2/orgs`);
  }

  // Server-Sent Events (SSE) Stream URL
  getEventsUrl(orgName: string, appName?: string): string {
    const base = appName
      ? `/v2/orgs/${encodeURIComponent(orgName)}/apps/${encodeURIComponent(appName)}/events`
      : `/v2/orgs/${encodeURIComponent(orgName)}/events`;
    if (this.apiKey) {
      return `${this.baseUrl}${base}?token=${encodeURIComponent(this.apiKey)}`;
    }
    return `${this.baseUrl}${base}`;
  }
}
