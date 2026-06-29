import type {
  APIErrorBody,
  ApplyPolicyInput,
  BackendAPIKeysResponse,
  ChangePasswordInput,
  ChangePasswordResponse,
  ClientSessionListResponse,
  DeployNodeInput,
  DesiredPolicyInput,
  DesiredPolicyResponse,
  LoginInput,
  LoginResponse,
  MeResponse,
  UpdateNodeInput,
  NodeCredentialInput,
  NodeCredentialResponse,
  NodeInput,
  NodeListResponse,
  PanelUserCreateInput,
  PanelUserListResponse,
  PanelUserPasswordResetInput,
  PanelUserResponse,
  PanelUserUpdateInput,
  PolicyRevisionInput,
  PolicyRevisionListResponse,
  PolicyRevisionResponse,
  PolicyValidationInput,
  PolicyValidationResponse,
  RepairAdminInput,
  RetryTaskResponse,
  SecurityOverviewResponse,
  SystemCheckResponse,
  TokenDefaultsInput,
  TokenDefaultsResponse,
  TrafficOverviewResponse,
  TuneUDPBufferInput,
  NodeReportsResponse,
  NodeConnectivityProbeResponse,
  NodeDiagnosticsResponse,
  NodeHMACSecretInput,
  NodeHMACSecretResponse,
  NodeNetworkDiagnosticsResponse,
  NodeResponse,
  NodeSyncStatusResponse,
  NodeTaskLogsResponse,
  NodeTaskResponse,
  NodeTasksResponse,
  SSHCredentialTestResponse
} from "./types";

export class PanelAPIError extends Error {
  status: number;
  code: string;

  constructor(status: number, code: string, message: string) {
    super(message);
    this.name = "PanelAPIError";
    this.status = status;
    this.code = code;
  }
}

const panelErrorMessageMap: Record<string, string> = {
  "request failed": "请求失败",
  "request failed with status": "请求失败，状态码",
  "method not allowed": "请求方法不允许",
  "permission denied": "当前账号没有执行该操作的权限",
  "login required": "请先登录控制面板",
  "missing or invalid API key": "API Key 缺失或无效",
  "node store is not configured": "节点存储未配置",
  "username or password is invalid": "账号或密码不正确",
  "create session failed": "创建登录会话失败",
  "refresh session failed": "刷新登录会话失败",
  "current password is invalid": "当前密码不正确",
  "new password must be different from current password": "新密码不能与当前密码相同",
  "hash password failed": "密码加密失败",
  "update password failed": "更新密码失败",
  "panel user not found": "面板账号不存在",
  "panel user already exists": "面板账号已存在",
  "admin cannot demote or disable itself": "管理员不能降低或禁用自己的权限",
  "use /api/panel/me/password to change your own password": "请在当前账号入口修改自己的密码",
  "list nodes failed": "读取节点列表失败",
  "get node failed": "读取节点失败",
  "node not found": "节点不存在",
  "node is not registered in panel": "节点尚未在控制面板登记",
  "body node_id does not match path node_id": "请求体里的节点 ID 与地址里的节点 ID 不一致",
  "upsert node failed": "保存节点失败",
  "delete node failed": "删除节点失败",
  "save report failed": "保存节点上报失败",
  "claim commands failed": "获取节点命令失败",
  "marshal commands failed": "生成节点命令失败",
  "sign commands failed": "签名节点命令失败",
  "list reports failed": "读取节点上报记录失败",
  "list tasks failed": "读取任务列表失败",
  "list task logs failed": "读取任务日志失败",
  "create task failed": "创建任务失败",
  "create policy task failed": "创建策略任务失败",
  "set desired policy failed": "设置目标策略失败",
  "get credential failed": "读取 SSH 凭据失败",
  "save credential failed": "保存 SSH 凭据失败",
  "delete credential failed": "删除 SSH 凭据失败",
  "credential not found": "SSH 凭据不存在",
  "node SSH credential is required before deploy": "部署前需要先保存节点 SSH 凭据",
  "node SSH credential is required before update": "更新前需要先保存节点 SSH 凭据",
  "node SSH credential is required before repairing admin access": "修复 Admin 接入前需要先保存节点 SSH 凭据",
  "node SSH credential is required before tuning UDP buffer": "优化 UDP Buffer 前需要先保存节点 SSH 凭据",
  "node hmac_secret is not configured; sync it from backend before deploy":
    "节点 HMAC Secret 尚未从业务后台同步，无法部署",
  "secret box is not configured": "密钥加密组件未配置",
  "get token defaults failed": "读取客户端默认配置失败"
};

const panelErrorCodeMap: Record<string, string> = {
  request_failed: "请求失败",
  method_not_allowed: "请求方法不允许",
  unauthorized: "登录已过期或鉴权失败",
  forbidden: "当前账号没有执行该操作的权限",
  login_required: "请先登录控制面板",
  invalid_credentials: "账号或密码不正确",
  login_rate_limited: "登录尝试过于频繁，请稍后再试",
  invalid_json: "请求 JSON 格式错误",
  invalid_body: "请求内容格式错误",
  invalid_node: "节点配置不合法",
  invalid_hmac_secret: "节点 HMAC Secret 不合法",
  node_id_mismatch: "节点 ID 不一致",
  node_not_found: "节点不存在",
  not_found: "数据不存在",
  store_error: "数据库操作失败",
  store_unavailable: "数据库存储未配置",
  session_error: "登录会话处理失败",
  invalid_current_password: "当前密码不正确",
  weak_password: "新密码强度不足",
  password_reused: "新密码不能与当前密码相同",
  password_hash_error: "密码加密失败",
  invalid_user: "账号信息不合法",
  invalid_user_id: "账号 ID 不合法",
  user_exists: "账号已存在",
  cannot_modify_self: "不能降低或禁用自己的账号",
  use_self_password_change: "请在当前账号入口修改自己的密码",
  credential_required: "需要先保存节点 SSH 凭据",
  credential_not_found: "SSH 凭据不存在",
  secret_box_unavailable: "密钥加密组件不可用",
  invalid_credential: "SSH 凭据不合法",
  invalid_task: "任务参数不合法",
  invalid_limit: "分页数量不合法",
  invalid_offset: "分页偏移不合法",
  invalid_policy_revision: "策略版本不合法",
  node_id_required: "缺少节点 ID",
  hmac_secret_required: "节点 HMAC Secret 尚未配置",
  marshal_error: "数据序列化失败",
  sign_error: "签名失败",
  invalid_token_defaults: "客户端默认配置不合法"
};

function translatePanelErrorMessage(status: number, code: string, message: string): string {
  const normalizedMessage = message.trim();
  if (normalizedMessage === "") {
    return panelErrorCodeMap[code] ?? `请求失败，状态码 ${status}`;
  }

  if (panelErrorMessageMap[normalizedMessage]) {
    return panelErrorMessageMap[normalizedMessage];
  }

  if (normalizedMessage.startsWith("request failed with status ")) {
    return `请求失败，状态码 ${normalizedMessage.replace("request failed with status ", "")}`;
  }

  const prefix = panelErrorCodeMap[code];
  if (!prefix) {
    return normalizedMessage;
  }

  if (/[\u4e00-\u9fff]/.test(normalizedMessage)) {
    return normalizedMessage;
  }

  if (code === "invalid_json") {
    return `${prefix}：${normalizedMessage}`;
  }

  if (code.startsWith("invalid_") || code === "weak_password") {
    return `${prefix}：${normalizedMessage}`;
  }

  return prefix;
}

declare global {
  interface Window {
    GACCEL_PANEL_CONFIG?: {
      apiBaseURL?: string;
    };
  }
}

const DEFAULT_API_BASE_URL = "http://103.201.131.99:8091";
const PANEL_TOKEN_STORAGE_KEY = "gaccel_panel_access_token";

let panelAccessToken =
  typeof window !== "undefined" ? window.localStorage.getItem(PANEL_TOKEN_STORAGE_KEY) ?? "" : "";

export function getPanelAccessToken(): string {
  return panelAccessToken;
}

export function setPanelAccessToken(token: string): void {
  panelAccessToken = token.trim();
  if (typeof window !== "undefined") {
    if (panelAccessToken === "") {
      window.localStorage.removeItem(PANEL_TOKEN_STORAGE_KEY);
    } else {
      window.localStorage.setItem(PANEL_TOKEN_STORAGE_KEY, panelAccessToken);
    }
  }
}

export function clearPanelAccessToken(): void {
  setPanelAccessToken("");
}

function apiBaseURL(): string {
  const configured = typeof window !== "undefined" ? window.GACCEL_PANEL_CONFIG?.apiBaseURL?.trim() ?? "" : "";
  return (configured || DEFAULT_API_BASE_URL).replace(/\/+$/, "");
}

export function getPanelAPIBaseURL(): string {
  return apiBaseURL();
}

function apiURL(path: string): string {
  const baseURL = apiBaseURL();
  return `${baseURL}${path.startsWith("/") ? path : `/${path}`}`;
}

async function request<T>(path: string, apiKey: string, init?: RequestInit): Promise<T> {
  const headers = new Headers(init?.headers);
  headers.set("Accept", "application/json");
  if (apiKey.trim() !== "") {
    headers.set("Authorization", `Bearer ${apiKey.trim()}`);
  } else if (path !== "/api/panel/login" && panelAccessToken !== "") {
    headers.set("Authorization", `Bearer ${panelAccessToken}`);
  }
  if (init?.body && !headers.has("Content-Type")) {
    headers.set("Content-Type", "application/json");
  }

  const response = await fetch(apiURL(path), {
    ...init,
    headers
  });
  const contentType = response.headers.get("Content-Type") ?? "";
  const body = contentType.includes("application/json")
    ? ((await response.json()) as T & APIErrorBody)
    : undefined;

  if (!response.ok) {
    const errorBody = body as APIErrorBody | undefined;
    throw new PanelAPIError(
      response.status,
      errorBody?.error?.code ?? "request_failed",
      translatePanelErrorMessage(
        response.status,
        errorBody?.error?.code ?? "request_failed",
        errorBody?.error?.message ?? `request failed with status ${response.status}`
      )
    );
  }
  return body as T;
}

export async function login(input: LoginInput): Promise<LoginResponse> {
  return request<LoginResponse>("/api/panel/login", "", {
    method: "POST",
    body: JSON.stringify(input)
  });
}

export async function logout(): Promise<{ status: string }> {
  return request<{ status: string }>("/api/panel/logout", "", {
    method: "POST"
  });
}

export async function getCurrentUser(): Promise<MeResponse> {
  return request<MeResponse>("/api/panel/me", "");
}

export async function changeCurrentPassword(input: ChangePasswordInput): Promise<ChangePasswordResponse> {
  return request<ChangePasswordResponse>("/api/panel/me/password", "", {
    method: "POST",
    body: JSON.stringify(input)
  });
}

export async function listPanelUsers(): Promise<PanelUserListResponse> {
  return request<PanelUserListResponse>("/api/panel/users", "");
}

export async function createPanelUser(input: PanelUserCreateInput): Promise<PanelUserResponse> {
  return request<PanelUserResponse>("/api/panel/users", "", {
    method: "POST",
    body: JSON.stringify(input)
  });
}

export async function updatePanelUser(userID: number, input: PanelUserUpdateInput): Promise<PanelUserResponse> {
  return request<PanelUserResponse>(`/api/panel/users/${encodeURIComponent(String(userID))}`, "", {
    method: "PUT",
    body: JSON.stringify(input)
  });
}

export async function resetPanelUserPassword(
  userID: number,
  input: PanelUserPasswordResetInput
): Promise<PanelUserResponse> {
  return request<PanelUserResponse>(`/api/panel/users/${encodeURIComponent(String(userID))}/password`, "", {
    method: "POST",
    body: JSON.stringify(input)
  });
}

export async function listNodes(apiKey: string, params: URLSearchParams): Promise<NodeListResponse> {
  const query = params.toString();
  return request<NodeListResponse>(`/api/panel/nodes${query ? `?${query}` : ""}`, apiKey);
}

export async function getNode(apiKey: string, nodeID: string): Promise<NodeResponse> {
  return request<NodeResponse>(`/api/panel/nodes/${encodeURIComponent(nodeID)}`, apiKey);
}

export async function createNode(apiKey: string, input: NodeInput): Promise<NodeResponse> {
  return request<NodeResponse>("/api/panel/nodes", apiKey, {
    method: "POST",
    body: JSON.stringify(input)
  });
}

export async function updateNode(apiKey: string, nodeID: string, input: NodeInput): Promise<NodeResponse> {
  return request<NodeResponse>(`/api/panel/nodes/${encodeURIComponent(nodeID)}`, apiKey, {
    method: "PUT",
    body: JSON.stringify(input)
  });
}

export async function deleteNode(apiKey: string, nodeID: string): Promise<{ status: string }> {
  return request<{ status: string }>(`/api/panel/nodes/${encodeURIComponent(nodeID)}`, apiKey, {
    method: "DELETE"
  });
}

export async function getNodeHMACSecretStatus(apiKey: string, nodeID: string): Promise<NodeHMACSecretResponse> {
  return request<NodeHMACSecretResponse>(`/api/panel/nodes/${encodeURIComponent(nodeID)}/hmac-secret`, apiKey);
}

export async function syncNodeHMACSecret(
  apiKey: string,
  nodeID: string,
  input: NodeHMACSecretInput
): Promise<NodeHMACSecretResponse> {
  return request<NodeHMACSecretResponse>(`/api/panel/nodes/${encodeURIComponent(nodeID)}/hmac-secret`, apiKey, {
    method: "PUT",
    body: JSON.stringify(input)
  });
}

export async function clearNodeHMACSecret(apiKey: string, nodeID: string): Promise<NodeHMACSecretResponse> {
  return request<NodeHMACSecretResponse>(`/api/panel/nodes/${encodeURIComponent(nodeID)}/hmac-secret`, apiKey, {
    method: "DELETE"
  });
}

export async function listNodeReports(apiKey: string, nodeID: string, limit = 10): Promise<NodeReportsResponse> {
  return request<NodeReportsResponse>(
    `/api/panel/nodes/${encodeURIComponent(nodeID)}/reports?limit=${encodeURIComponent(String(limit))}`,
    apiKey
  );
}

export async function listNodeTasks(apiKey: string, nodeID: string, limit = 20): Promise<NodeTasksResponse> {
  return request<NodeTasksResponse>(
    `/api/panel/nodes/${encodeURIComponent(nodeID)}/tasks?limit=${encodeURIComponent(String(limit))}`,
    apiKey
  );
}

export async function createApplyPolicyTask(
  apiKey: string,
  nodeID: string,
  input: ApplyPolicyInput
): Promise<NodeTaskResponse> {
  return request<NodeTaskResponse>(`/api/panel/nodes/${encodeURIComponent(nodeID)}/commands/apply_policy`, apiKey, {
    method: "POST",
    body: JSON.stringify(input)
  });
}

export async function listPolicyRevisions(apiKey: string, limit = 50): Promise<PolicyRevisionListResponse> {
  return request<PolicyRevisionListResponse>(
    `/api/panel/policy-revisions?limit=${encodeURIComponent(String(limit))}`,
    apiKey
  );
}

export async function savePolicyRevision(
  apiKey: string,
  input: PolicyRevisionInput
): Promise<PolicyRevisionResponse> {
  return request<PolicyRevisionResponse>("/api/panel/policy-revisions", apiKey, {
    method: "POST",
    body: JSON.stringify(input)
  });
}

export async function validatePolicyRevision(
  apiKey: string,
  input: PolicyValidationInput
): Promise<PolicyValidationResponse> {
  return request<PolicyValidationResponse>("/api/panel/policy-revisions/validate", apiKey, {
    method: "POST",
    body: JSON.stringify(input)
  });
}

export async function setNodeDesiredPolicy(
  apiKey: string,
  nodeID: string,
  input: DesiredPolicyInput
): Promise<DesiredPolicyResponse> {
  return request<DesiredPolicyResponse>(`/api/panel/nodes/${encodeURIComponent(nodeID)}/desired-policy`, apiKey, {
    method: "POST",
    body: JSON.stringify(input)
  });
}

export async function createDeployTask(
  apiKey: string,
  nodeID: string,
  input: DeployNodeInput
): Promise<NodeTaskResponse> {
  return request<NodeTaskResponse>(`/api/panel/nodes/${encodeURIComponent(nodeID)}/deploy`, apiKey, {
    method: "POST",
    body: JSON.stringify(input)
  });
}

export async function createUpdateTask(
  apiKey: string,
  nodeID: string,
  input: UpdateNodeInput
): Promise<NodeTaskResponse> {
  return request<NodeTaskResponse>(`/api/panel/nodes/${encodeURIComponent(nodeID)}/update`, apiKey, {
    method: "POST",
    body: JSON.stringify(input)
  });
}

export async function createRepairAdminTask(
  apiKey: string,
  nodeID: string,
  input: RepairAdminInput = {}
): Promise<NodeTaskResponse> {
  return request<NodeTaskResponse>(`/api/panel/nodes/${encodeURIComponent(nodeID)}/repair-admin`, apiKey, {
    method: "POST",
    body: JSON.stringify(input)
  });
}

export async function createTuneUDPBufferTask(
  apiKey: string,
  nodeID: string,
  input: TuneUDPBufferInput = {}
): Promise<NodeTaskResponse> {
  return request<NodeTaskResponse>(`/api/panel/nodes/${encodeURIComponent(nodeID)}/tune-udp-buffer`, apiKey, {
    method: "POST",
    body: JSON.stringify(input)
  });
}

export async function listTaskLogs(apiKey: string, taskID: string, limit = 300): Promise<NodeTaskLogsResponse> {
  return request<NodeTaskLogsResponse>(
    `/api/panel/tasks/${encodeURIComponent(taskID)}/logs?limit=${encodeURIComponent(String(limit))}`,
    apiKey
  );
}

export async function retryTask(apiKey: string, taskID: string, priority?: number): Promise<RetryTaskResponse> {
  return request<RetryTaskResponse>(`/api/panel/tasks/${encodeURIComponent(taskID)}/retry`, apiKey, {
    method: "POST",
    body: JSON.stringify(priority ? { priority } : {})
  });
}

export async function getNodeSyncStatus(apiKey: string, nodeID: string): Promise<NodeSyncStatusResponse> {
  return request<NodeSyncStatusResponse>(`/api/panel/nodes/${encodeURIComponent(nodeID)}/sync-status`, apiKey);
}

export async function getNodeDiagnostics(apiKey: string, nodeID: string): Promise<NodeDiagnosticsResponse> {
  return request<NodeDiagnosticsResponse>(`/api/panel/nodes/${encodeURIComponent(nodeID)}/diagnostics`, apiKey);
}

export async function getNodeNetworkDiagnostics(
  apiKey: string,
  nodeID: string
): Promise<NodeNetworkDiagnosticsResponse> {
  return request<NodeNetworkDiagnosticsResponse>(
    `/api/panel/nodes/${encodeURIComponent(nodeID)}/network-diagnostics`,
    apiKey
  );
}

export async function getNodeConnectivityProbe(
  apiKey: string,
  nodeID: string
): Promise<NodeConnectivityProbeResponse> {
  return request<NodeConnectivityProbeResponse>(
    `/api/panel/nodes/${encodeURIComponent(nodeID)}/connectivity-probe`,
    apiKey
  );
}

export async function getNodeCredential(apiKey: string, nodeID: string): Promise<NodeCredentialResponse> {
  return request<NodeCredentialResponse>(`/api/panel/nodes/${encodeURIComponent(nodeID)}/credential`, apiKey);
}

export async function saveNodeCredential(
  apiKey: string,
  nodeID: string,
  input: NodeCredentialInput
): Promise<NodeCredentialResponse> {
  return request<NodeCredentialResponse>(`/api/panel/nodes/${encodeURIComponent(nodeID)}/credential`, apiKey, {
    method: "PUT",
    body: JSON.stringify(input)
  });
}

export async function deleteNodeCredential(apiKey: string, nodeID: string): Promise<{ status: string }> {
  return request<{ status: string }>(`/api/panel/nodes/${encodeURIComponent(nodeID)}/credential`, apiKey, {
    method: "DELETE"
  });
}

export async function testNodeCredential(apiKey: string, nodeID: string): Promise<SSHCredentialTestResponse> {
  return request<SSHCredentialTestResponse>(`/api/panel/nodes/${encodeURIComponent(nodeID)}/credential/test`, apiKey, {
    method: "POST"
  });
}

export async function getSecurityOverview(): Promise<SecurityOverviewResponse> {
  return request<SecurityOverviewResponse>("/api/panel/security/overview", "");
}

export async function getBackendAPIKeys(): Promise<BackendAPIKeysResponse> {
  return request<BackendAPIKeysResponse>("/api/panel/security/backend-api-keys", "");
}

export async function getSystemCheck(): Promise<SystemCheckResponse> {
  return request<SystemCheckResponse>("/api/panel/system/check", "");
}

export async function getTokenDefaults(): Promise<TokenDefaultsResponse> {
  return request<TokenDefaultsResponse>("/api/panel/token-defaults", "");
}

export async function saveTokenDefaults(input: TokenDefaultsInput): Promise<TokenDefaultsResponse> {
  return request<TokenDefaultsResponse>("/api/panel/token-defaults", "", {
    method: "PUT",
    body: JSON.stringify(input)
  });
}

export async function getTrafficOverview(windowHours = 24, limit = 20): Promise<TrafficOverviewResponse> {
  const params = new URLSearchParams({
    window_hours: String(windowHours),
    limit: String(limit)
  });
  return request<TrafficOverviewResponse>(`/api/panel/traffic/overview?${params.toString()}`, "");
}

export async function listClientSessions(params: URLSearchParams): Promise<ClientSessionListResponse> {
  const query = params.toString();
  return request<ClientSessionListResponse>(`/api/panel/client-sessions${query ? `?${query}` : ""}`, "");
}
