import { useCallback, useEffect, useMemo, useState } from "react";
import { Alert, Button, Input, Layout, Select, Space, Spin, Statistic, Tag, Typography, message } from "antd";
import {
  BarChart3,
  Clock3,
  KeyRound,
  LogOut,
  Plus,
  RefreshCw,
  Route,
  Search,
  Server,
  ShieldCheck,
  Signal,
  UserRound,
  UsersRound,
  Wrench
} from "lucide-react";
import {
  changeCurrentPassword,
  createDeployTask,
  createNode,
  createRepairAdminTask,
  createTuneUDPBufferTask,
  createUpdateTask,
  clearPanelAccessToken,
  deleteNode,
  deleteNodeCredential,
  getCurrentUser,
  getPanelAccessToken,
  getNode,
  getNodeConnectivityProbe,
  getNodeCredential,
  getNodeDiagnostics,
  getNodeNetworkDiagnostics,
  getNodeSyncStatus,
  listNodeReports,
  listNodes,
  listNodeTasks,
  listPolicyRevisions,
  listTaskLogs,
  login,
  logout,
  PanelAPIError,
  retryTask,
  saveNodeCredential,
  setNodeDesiredPolicy,
  setPanelAccessToken,
  testNodeCredential,
  updateNode
} from "./api";
import { ChangePasswordModal } from "./components/ChangePasswordModal";
import { ClientSessionsPanel } from "./components/ClientSessionsPanel";
import { CredentialModal } from "./components/CredentialModal";
import { DeployNodeModal } from "./components/DeployNodeModal";
import { LoginScreen } from "./components/LoginScreen";
import { NodeDetailDrawer } from "./components/NodeDetailDrawer";
import { NodeDiagnosticsModal } from "./components/NodeDiagnosticsModal";
import { NodeFormModal } from "./components/NodeFormModal";
import { NodeTable } from "./components/NodeTable";
import { PolicyManagementPanel } from "./components/PolicyManagementPanel";
import { SyncPolicyModal } from "./components/SyncPolicyModal";
import { SystemCheckPanel } from "./components/SystemCheckPanel";
import { TaskLogDrawer } from "./components/TaskLogDrawer";
import { TrafficOverviewPanel } from "./components/TrafficOverviewPanel";
import { UpdateNodeModal } from "./components/UpdateNodeModal";
import { UserManagementPanel } from "./components/UserManagementPanel";
import type {
  ChangePasswordInput,
  DeployNodeInput,
  DesiredPolicyInput,
  LoginInput,
  NodeCredential,
  NodeCredentialInput,
  NodeConnectivityProbeResponse,
  NodeInput,
  NodeReport,
  NodeDiagnosticsResponse,
  NodeNetworkDiagnosticsResponse,
  NodeSyncStatus,
  NodeStatus,
  NodeTask,
  NodeTaskLog,
  PanelNode,
  PanelUser,
  PolicyRevision,
  TuneUDPBufferInput,
  UpdateNodeInput
} from "./types";

const { Content } = Layout;
const { Text, Title } = Typography;
const SESSION_API_KEY = "";

type AppView = "nodes" | "traffic" | "sessions" | "users" | "policies" | "system";

function isUnauthorized(error: unknown) {
  return error instanceof PanelAPIError && error.status === 401;
}

function authErrorText(error: unknown) {
  if (isUnauthorized(error)) {
    return "登录已过期或账号无权限，请重新登录";
  }
  if (error instanceof PanelAPIError && error.status === 403) {
    return "当前账号没有执行该操作的权限";
  }
  if (error instanceof Error) {
    return error.message;
  }
  return "请求失败";
}

function roleText(role: string) {
  switch (role) {
    case "admin":
      return "管理员";
    case "operator":
      return "操作员";
    case "viewer":
      return "观察员";
    default:
      return role || "-";
  }
}

function pageTitle(view: AppView) {
  switch (view) {
    case "traffic":
      return "流量与联调观测";
    case "sessions":
      return "客户端会话";
    case "users":
      return "账号与权限";
    case "policies":
      return "策略与游戏配置";
    case "system":
      return "系统自检";
    default:
      return "节点控制面板";
  }
}

function hasValueDrift(currentValue?: string, desiredValue?: string) {
  const desired = desiredValue?.trim() ?? "";
  return desired !== "" && (currentValue?.trim() ?? "") !== desired;
}

export default function App() {
  const [currentUser, setCurrentUser] = useState<PanelUser | null>(null);
  const [authChecking, setAuthChecking] = useState(true);
  const [loginLoading, setLoginLoading] = useState(false);
  const [loginError, setLoginError] = useState("");
  const [activeView, setActiveView] = useState<AppView>("nodes");
  const [passwordOpen, setPasswordOpen] = useState(false);
  const [passwordSubmitting, setPasswordSubmitting] = useState(false);
  const [passwordError, setPasswordError] = useState("");
  const [nodes, setNodes] = useState<PanelNode[]>([]);
  const [loading, setLoading] = useState(false);
  const [submitting, setSubmitting] = useState(false);
  const [query, setQuery] = useState("");
  const [status, setStatus] = useState<NodeStatus | "">("");
  const [region, setRegion] = useState("");
  const [formOpen, setFormOpen] = useState(false);
  const [editingNode, setEditingNode] = useState<PanelNode | undefined>();
  const [detailNode, setDetailNode] = useState<PanelNode | undefined>();
  const [detailReports, setDetailReports] = useState<NodeReport[]>([]);
  const [detailTasks, setDetailTasks] = useState<NodeTask[]>([]);
  const [detailSyncStatus, setDetailSyncStatus] = useState<NodeSyncStatus | null>(null);
  const [detailCredential, setDetailCredential] = useState<NodeCredential | null>(null);
  const [detailLoading, setDetailLoading] = useState(false);
  const [detailOpen, setDetailOpen] = useState(false);
  const [applyPolicyNode, setApplyPolicyNode] = useState<PanelNode | undefined>();
  const [applyPolicyOpen, setApplyPolicyOpen] = useState(false);
  const [applyingPolicy, setApplyingPolicy] = useState(false);
  const [policyRevisions, setPolicyRevisions] = useState<PolicyRevision[]>([]);
  const [policyRevisionsLoading, setPolicyRevisionsLoading] = useState(false);
  const [deployNode, setDeployNode] = useState<PanelNode | undefined>();
  const [deployOpen, setDeployOpen] = useState(false);
  const [deploying, setDeploying] = useState(false);
  const [targetUpdateNode, setTargetUpdateNode] = useState<PanelNode | undefined>();
  const [updateOpen, setUpdateOpen] = useState(false);
  const [updating, setUpdating] = useState(false);
  const [credentialNode, setCredentialNode] = useState<PanelNode | undefined>();
  const [credentialOpen, setCredentialOpen] = useState(false);
  const [savingCredential, setSavingCredential] = useState(false);
  const [deletingCredential, setDeletingCredential] = useState(false);
  const [testingCredential, setTestingCredential] = useState(false);
  const [taskLogOpen, setTaskLogOpen] = useState(false);
  const [taskLogTask, setTaskLogTask] = useState<NodeTask | undefined>();
  const [taskLogs, setTaskLogs] = useState<NodeTaskLog[]>([]);
  const [taskLogsLoading, setTaskLogsLoading] = useState(false);
  const [diagnosticNode, setDiagnosticNode] = useState<PanelNode | undefined>();
  const [diagnosticOpen, setDiagnosticOpen] = useState(false);
  const [diagnostics, setDiagnostics] = useState<NodeDiagnosticsResponse | null>(null);
  const [diagnosticsLoading, setDiagnosticsLoading] = useState(false);
  const [connectivityProbe, setConnectivityProbe] = useState<NodeConnectivityProbeResponse | null>(null);
  const [connectivityProbeLoading, setConnectivityProbeLoading] = useState(false);
  const [networkDiagnostics, setNetworkDiagnostics] = useState<NodeNetworkDiagnosticsResponse | null>(null);
  const [networkDiagnosticsLoading, setNetworkDiagnosticsLoading] = useState(false);
  const [repairingAdmin, setRepairingAdmin] = useState(false);
  const [tuningUDPBuffer, setTuningUDPBuffer] = useState(false);
  const [error, setError] = useState("");
  const [messageAPI, contextHolder] = message.useMessage();

  const canManage = currentUser?.role === "admin";

  const filteredRegions = useMemo(
    () =>
      Array.from(new Set(nodes.map((node) => node.region).filter(Boolean))).map((value) => ({
        value,
        label: value
      })),
    [nodes]
  );

  const metrics = useMemo(() => {
    const online = nodes.filter((node) => node.status === "online").length;
    const warning = nodes.filter((node) => node.status === "error" || node.status === "offline").length;
    const tcp = nodes.filter((node) => node.allow_tcp).length;
    const udp = nodes.filter((node) => node.allow_udp).length;
    const versionPending = nodes.filter((node) => hasValueDrift(node.current_version, node.desired_version)).length;
    const policyPending = nodes.filter((node) =>
      hasValueDrift(node.current_policy_revision, node.desired_policy_revision)
    ).length;
    return { online, warning, tcp, udp, versionPending, policyPending };
  }, [nodes]);

  const resetWorkspace = useCallback(() => {
    setActiveView("nodes");
    setNodes([]);
    setDetailNode(undefined);
    setDetailReports([]);
    setDetailTasks([]);
    setDetailSyncStatus(null);
    setDetailCredential(null);
    setDetailOpen(false);
    setFormOpen(false);
    setApplyPolicyOpen(false);
    setDeployOpen(false);
    setUpdateOpen(false);
    setCredentialOpen(false);
    setTaskLogOpen(false);
    setDiagnosticOpen(false);
    setDiagnosticNode(undefined);
    setDiagnostics(null);
    setConnectivityProbe(null);
    setNetworkDiagnostics(null);
  }, []);

  const loadNodes = useCallback(async () => {
    if (!currentUser) {
      setNodes([]);
      setError("");
      return;
    }
    setLoading(true);
    setError("");
    try {
      const params = new URLSearchParams();
      if (query.trim()) params.set("q", query.trim());
      if (status) params.set("status", status);
      if (region.trim()) params.set("region", region.trim());
      const response = await listNodes(SESSION_API_KEY, params);
      setNodes(response.nodes);
    } catch (err) {
      if (isUnauthorized(err)) {
        clearPanelAccessToken();
        setCurrentUser(null);
        resetWorkspace();
        setLoginError(authErrorText(err));
      } else {
        setError(authErrorText(err));
      }
    } finally {
      setLoading(false);
    }
  }, [currentUser, query, region, resetWorkspace, status]);

  useEffect(() => {
    let active = true;
    if (getPanelAccessToken() === "") {
      setCurrentUser(null);
      setAuthChecking(false);
      return () => {
        active = false;
      };
    }
    void getCurrentUser()
      .then((response) => {
        if (active) {
          setCurrentUser(response.user);
        }
      })
      .catch(() => {
        if (active) {
          clearPanelAccessToken();
          setCurrentUser(null);
        }
      })
      .finally(() => {
        if (active) {
          setAuthChecking(false);
        }
      });
    return () => {
      active = false;
    };
  }, []);

  useEffect(() => {
    void loadNodes();
  }, [loadNodes]);

  useEffect(() => {
    if (currentUser && currentUser.role !== "admin" && activeView !== "nodes") {
      setActiveView("nodes");
    }
  }, [activeView, currentUser]);

  const handlePanelError = useCallback(
    (err: unknown) => {
      if (isUnauthorized(err)) {
        clearPanelAccessToken();
        setCurrentUser(null);
        resetWorkspace();
        setLoginError(authErrorText(err));
        return;
      }
      messageAPI.error(authErrorText(err));
    },
    [messageAPI, resetWorkspace]
  );

  const ensureManage = () => {
    if (!canManage) {
      messageAPI.warning("当前账号只能查看节点状态");
      return false;
    }
    return true;
  };

  const submitLogin = async (input: LoginInput) => {
    setLoginLoading(true);
    setLoginError("");
    try {
      const response = await login(input);
      setPanelAccessToken(response.access_token || response.token);
      setCurrentUser(response.user);
      setActiveView("nodes");
      messageAPI.success("登录成功");
    } catch (err) {
      setLoginError(isUnauthorized(err) ? "账号或密码不正确" : authErrorText(err));
    } finally {
      setLoginLoading(false);
    }
  };

  const submitLogout = async () => {
    try {
      await logout();
    } catch {
      // 本地状态仍然清理，兼容服务端 token 已过期的情况。
    }
    clearPanelAccessToken();
    setCurrentUser(null);
    setLoginError("");
    resetWorkspace();
    messageAPI.success("已退出登录");
  };

  const submitChangePassword = async (input: ChangePasswordInput) => {
    setPasswordSubmitting(true);
    setPasswordError("");
    try {
      const response = await changeCurrentPassword(input);
      setPanelAccessToken(response.access_token || response.token);
      setCurrentUser(response.user);
      setPasswordOpen(false);
      messageAPI.success("密码已更新");
    } catch (err) {
      if (err instanceof PanelAPIError && err.code === "invalid_current_password") {
        setPasswordError("当前密码不正确");
      } else if (err instanceof PanelAPIError && err.code === "weak_password") {
        setPasswordError("新密码至少 10 位，且不能以空格开头或结尾");
      } else if (err instanceof PanelAPIError && err.code === "password_reused") {
        setPasswordError("新密码不能与当前密码相同");
      } else {
        setPasswordError(authErrorText(err));
      }
    } finally {
      setPasswordSubmitting(false);
    }
  };

  const openCreate = () => {
    if (!ensureManage()) {
      return;
    }
    setEditingNode(undefined);
    setFormOpen(true);
  };

  const openEdit = (node: PanelNode) => {
    if (!ensureManage()) {
      return;
    }
    setEditingNode(node);
    setFormOpen(true);
  };

  const loadNodeDetail = useCallback(
    async (nodeID: string) => {
      setDetailLoading(true);
      try {
        const [nodeResponse, reportsResponse, tasksResponse, syncStatusResponse, credentialResponse] = await Promise.all([
          getNode(SESSION_API_KEY, nodeID),
          listNodeReports(SESSION_API_KEY, nodeID),
          listNodeTasks(SESSION_API_KEY, nodeID),
          getNodeSyncStatus(SESSION_API_KEY, nodeID),
          canManage
            ? getNodeCredential(SESSION_API_KEY, nodeID)
            : Promise.resolve({ credential: null as NodeCredential | null })
        ]);
        setDetailNode(nodeResponse.node);
        setDetailReports(reportsResponse.reports);
        setDetailTasks(tasksResponse.tasks);
        setDetailSyncStatus(syncStatusResponse.sync_status);
        setDetailCredential(credentialResponse.credential);
      } catch (err) {
        if (isUnauthorized(err)) {
          clearPanelAccessToken();
          setCurrentUser(null);
          resetWorkspace();
          setLoginError(authErrorText(err));
        } else {
          messageAPI.error(authErrorText(err));
        }
      } finally {
        setDetailLoading(false);
      }
    },
    [canManage, messageAPI, resetWorkspace]
  );

  const openDetail = (node: PanelNode) => {
    setDetailNode(node);
    setDetailReports([]);
    setDetailTasks([]);
    setDetailSyncStatus(null);
    setDetailCredential(null);
    setDetailOpen(true);
    void loadNodeDetail(node.node_id);
  };

  const openDiagnostics = async (node: PanelNode) => {
    setDiagnosticNode(node);
    setDiagnostics(null);
    setConnectivityProbe(null);
    setNetworkDiagnostics(null);
    setDiagnosticOpen(true);
    setDiagnosticsLoading(true);
    try {
      const response = await getNodeDiagnostics(SESSION_API_KEY, node.node_id);
      setDiagnostics({
        ...response,
        checks: Array.isArray(response.checks) ? response.checks : [],
        recommendations: Array.isArray(response.recommendations) ? response.recommendations : []
      });
    } catch (err) {
      handlePanelError(err);
    } finally {
      setDiagnosticsLoading(false);
    }
  };

  const repairAdminAccess = async (node?: PanelNode) => {
    if (!node || !ensureManage()) {
      return;
    }
    setRepairingAdmin(true);
    try {
      const response = await createRepairAdminTask(SESSION_API_KEY, node.node_id, { listen_host: "0.0.0.0" });
      messageAPI.success(`Admin 修复任务已创建：${response.task.task_id}`);
      await loadNodes();
      if (detailOpen && detailNode?.node_id === node.node_id) {
        await loadNodeDetail(node.node_id);
      }
    } catch (err) {
      messageAPI.error(authErrorText(err));
    } finally {
      setRepairingAdmin(false);
    }
  };

  const runNetworkDiagnostics = async (node?: PanelNode) => {
    if (!node || !ensureManage()) {
      return;
    }
    setNetworkDiagnosticsLoading(true);
    try {
      const response = await getNodeNetworkDiagnostics(SESSION_API_KEY, node.node_id);
      setNetworkDiagnostics({
        ...response,
        checks: Array.isArray(response.checks) ? response.checks : [],
        recommendations: Array.isArray(response.recommendations) ? response.recommendations : []
      });
      messageAPI.success("网络体检完成");
    } catch (err) {
      messageAPI.error(authErrorText(err));
    } finally {
      setNetworkDiagnosticsLoading(false);
    }
  };

  const runConnectivityProbe = async (node?: PanelNode) => {
    if (!node || !ensureManage()) {
      return;
    }
    setConnectivityProbeLoading(true);
    try {
      const response = await getNodeConnectivityProbe(SESSION_API_KEY, node.node_id);
      setConnectivityProbe({
        ...response,
        checks: Array.isArray(response.checks) ? response.checks : [],
        recommendations: Array.isArray(response.recommendations) ? response.recommendations : []
      });
      messageAPI.success("主动探测完成");
    } catch (err) {
      messageAPI.error(authErrorText(err));
    } finally {
      setConnectivityProbeLoading(false);
    }
  };

  const tuneUDPBuffer = async (node?: PanelNode, input: TuneUDPBufferInput = {}) => {
    if (!node || !ensureManage()) {
      return;
    }
    setTuningUDPBuffer(true);
    try {
      const response = await createTuneUDPBufferTask(SESSION_API_KEY, node.node_id, input);
      messageAPI.success(`UDP Buffer 优化任务已创建：${response.task.task_id}`);
      await loadNodes();
      if (detailOpen && detailNode?.node_id === node.node_id) {
        await loadNodeDetail(node.node_id);
      }
    } catch (err) {
      messageAPI.error(authErrorText(err));
    } finally {
      setTuningUDPBuffer(false);
    }
  };

  const refreshDetail = () => {
    if (detailNode) {
      void loadNodeDetail(detailNode.node_id);
    }
  };

  const openApplyPolicy = (node: PanelNode) => {
    if (!ensureManage()) {
      return;
    }
    setApplyPolicyNode(node);
    setApplyPolicyOpen(true);
    setPolicyRevisionsLoading(true);
    void listPolicyRevisions(SESSION_API_KEY)
      .then((response) => setPolicyRevisions(response.policy_revisions))
      .catch((err) => {
        if (isUnauthorized(err)) {
          clearPanelAccessToken();
          setCurrentUser(null);
          resetWorkspace();
          setLoginError(authErrorText(err));
          return;
        }
        setPolicyRevisions([]);
        messageAPI.error(authErrorText(err));
      })
      .finally(() => setPolicyRevisionsLoading(false));
  };

  const openCredential = (node: PanelNode) => {
    if (!ensureManage()) {
      return;
    }
    setCredentialNode(node);
    setCredentialOpen(true);
  };

  const openDeploy = (node: PanelNode) => {
    if (!ensureManage()) {
      return;
    }
    setDeployNode(node);
    setDeployOpen(true);
  };

  const openUpdate = (node: PanelNode) => {
    if (!ensureManage()) {
      return;
    }
    setTargetUpdateNode(node);
    setUpdateOpen(true);
  };

  const submitDeploy = async (input: DeployNodeInput) => {
    if (!deployNode || !ensureManage()) {
      return;
    }
    setDeploying(true);
    try {
      await createDeployTask(SESSION_API_KEY, deployNode.node_id, input);
      messageAPI.success("部署任务已创建，可在节点详情页查看任务日志");
      setDeployOpen(false);
      await loadNodes();
      if (detailOpen && detailNode?.node_id === deployNode.node_id) {
        await loadNodeDetail(deployNode.node_id);
      }
    } catch (err) {
      messageAPI.error(authErrorText(err));
      throw err;
    } finally {
      setDeploying(false);
    }
  };

  const submitUpdate = async (input: UpdateNodeInput) => {
    if (!targetUpdateNode || !ensureManage()) {
      return;
    }
    setUpdating(true);
    try {
      await createUpdateTask(SESSION_API_KEY, targetUpdateNode.node_id, input);
      messageAPI.success("更新任务已创建，可在节点详情页查看任务日志");
      setUpdateOpen(false);
      await loadNodes();
      if (detailOpen && detailNode?.node_id === targetUpdateNode.node_id) {
        await loadNodeDetail(targetUpdateNode.node_id);
      }
    } catch (err) {
      messageAPI.error(authErrorText(err));
      throw err;
    } finally {
      setUpdating(false);
    }
  };

  const openTaskLogs = async (task: NodeTask) => {
    setTaskLogTask(task);
    setTaskLogs([]);
    setTaskLogOpen(true);
    setTaskLogsLoading(true);
    try {
      const response = await listTaskLogs(SESSION_API_KEY, task.task_id);
      setTaskLogs(response.logs);
    } catch (err) {
      messageAPI.error(authErrorText(err));
    } finally {
      setTaskLogsLoading(false);
    }
  };

  const retryNodeTask = async (task: NodeTask) => {
    if (!ensureManage()) {
      return;
    }
    try {
      const response = await retryTask(SESSION_API_KEY, task.task_id);
      messageAPI.success(`重试任务已创建：${response.task.task_id}`);
      if (detailNode) {
        await loadNodeDetail(detailNode.node_id);
      }
      await loadNodes();
    } catch (err) {
      messageAPI.error(authErrorText(err));
    }
  };

  const submitCredential = async (input: NodeCredentialInput) => {
    if (!credentialNode || !ensureManage()) {
      return;
    }
    setSavingCredential(true);
    try {
      const response = await saveNodeCredential(SESSION_API_KEY, credentialNode.node_id, input);
      messageAPI.success("SSH 凭据已保存");
      setCredentialOpen(false);
      if (detailOpen && detailNode?.node_id === credentialNode.node_id) {
        setDetailCredential(response.credential);
      }
    } catch (err) {
      messageAPI.error(authErrorText(err));
      throw err;
    } finally {
      setSavingCredential(false);
    }
  };

  const removeCredential = async () => {
    if (!credentialNode || !ensureManage()) {
      return;
    }
    setDeletingCredential(true);
    try {
      await deleteNodeCredential(SESSION_API_KEY, credentialNode.node_id);
      messageAPI.success("SSH 凭据已删除");
      setCredentialOpen(false);
      if (detailOpen && detailNode?.node_id === credentialNode.node_id) {
        setDetailCredential(null);
      }
    } catch (err) {
      messageAPI.error(authErrorText(err));
      throw err;
    } finally {
      setDeletingCredential(false);
    }
  };

  const testCredential = async (node: PanelNode) => {
    if (!ensureManage()) {
      return;
    }
    setTestingCredential(true);
    try {
      const response = await testNodeCredential(SESSION_API_KEY, node.node_id);
      if (response.result.ok) {
        messageAPI.success(`SSH 测试成功：${response.result.latency_ms}ms`);
      } else {
        messageAPI.error(response.result.error || "SSH 测试失败");
      }
      await loadNodeDetail(node.node_id);
    } catch (err) {
      messageAPI.error(authErrorText(err));
    } finally {
      setTestingCredential(false);
    }
  };

  const submitApplyPolicy = async (input: DesiredPolicyInput) => {
    if (!applyPolicyNode || !ensureManage()) {
      return;
    }
    setApplyingPolicy(true);
    try {
      await setNodeDesiredPolicy(SESSION_API_KEY, applyPolicyNode.node_id, input);
      messageAPI.success("策略任务已创建，等待节点拉取");
      setApplyPolicyOpen(false);
      await loadNodes();
      if (detailOpen && detailNode?.node_id === applyPolicyNode.node_id) {
        await loadNodeDetail(applyPolicyNode.node_id);
      }
    } catch (err) {
      messageAPI.error(authErrorText(err));
      throw err;
    } finally {
      setApplyingPolicy(false);
    }
  };

  const submitNode = async (input: NodeInput) => {
    if (!ensureManage()) {
      return;
    }
    setSubmitting(true);
    try {
      if (editingNode) {
        await updateNode(SESSION_API_KEY, editingNode.node_id, input);
        messageAPI.success("节点已保存");
      } else {
        await createNode(SESSION_API_KEY, input);
        messageAPI.success("节点已创建");
      }
      setFormOpen(false);
      await loadNodes();
    } catch (err) {
      messageAPI.error(authErrorText(err));
      throw err;
    } finally {
      setSubmitting(false);
    }
  };

  const removeNode = async (node: PanelNode) => {
    if (!ensureManage()) {
      return;
    }
    try {
      await deleteNode(SESSION_API_KEY, node.node_id);
      messageAPI.success("节点已删除");
      await loadNodes();
    } catch (err) {
      messageAPI.error(authErrorText(err));
    }
  };

  if (authChecking) {
    return (
      <Layout className="app-root">
        {contextHolder}
        <div className="auth-loading">
          <Spin />
          <span>正在检查登录状态</span>
        </div>
      </Layout>
    );
  }

  if (!currentUser) {
    return (
      <>
        {contextHolder}
        <LoginScreen loading={loginLoading} error={loginError} onSubmit={submitLogin} />
      </>
    );
  }

  return (
    <Layout className="app-root">
      {contextHolder}
      <aside className="side-rail">
        <div className="rail-logo">
          <Server size={22} />
        </div>
        <button
          className={`rail-item ${activeView === "nodes" ? "active" : ""}`}
          type="button"
          aria-label="节点"
          onClick={() => setActiveView("nodes")}
        >
          <Signal size={18} />
        </button>
        <button
          className={`rail-item ${activeView === "traffic" ? "active" : ""}`}
          type="button"
          aria-label="流量"
          onClick={() => setActiveView("traffic")}
        >
          <BarChart3 size={18} />
        </button>
        <button
          className={`rail-item ${activeView === "sessions" ? "active" : ""}`}
          type="button"
          aria-label="客户端会话"
          onClick={() => setActiveView("sessions")}
        >
          <Clock3 size={18} />
        </button>
        {canManage && (
          <button
            className={`rail-item ${activeView === "policies" ? "active" : ""}`}
            type="button"
            aria-label="策略"
            onClick={() => setActiveView("policies")}
          >
            <Route size={18} />
          </button>
        )}
        {canManage && (
          <button
            className={`rail-item ${activeView === "users" ? "active" : ""}`}
            type="button"
            aria-label="账号"
            onClick={() => setActiveView("users")}
          >
            <UsersRound size={18} />
          </button>
        )}
        {canManage && (
          <button
            className={`rail-item ${activeView === "system" ? "active" : ""}`}
            type="button"
            aria-label="系统自检"
            onClick={() => setActiveView("system")}
          >
            <Wrench size={18} />
          </button>
        )}
        <div className="rail-foot">
          <ShieldCheck size={18} />
        </div>
      </aside>

      <Content className="panel-content">
        <header className="topbar">
          <div>
            <Text className="eyebrow">gaccel panel</Text>
            <Title level={2}>{pageTitle(activeView)}</Title>
          </div>
          <Space className="user-box" align="center">
            <UserRound size={17} />
            <div className="user-meta">
              <Text>当前账号</Text>
              <strong>{currentUser.username}</strong>
            </div>
            <Tag color={canManage ? "blue" : "default"}>{roleText(currentUser.role)}</Tag>
            <Button icon={<KeyRound size={16} />} onClick={() => setPasswordOpen(true)}>
              改密
            </Button>
            <Button icon={<LogOut size={16} />} onClick={() => void submitLogout()}>
              退出
            </Button>
          </Space>
        </header>

        {activeView === "traffic" ? (
          <TrafficOverviewPanel onRequestError={handlePanelError} />
        ) : activeView === "sessions" ? (
          <ClientSessionsPanel nodes={nodes} onRequestError={handlePanelError} />
        ) : activeView === "users" && canManage ? (
          <UserManagementPanel currentUser={currentUser} />
        ) : activeView === "policies" && canManage ? (
          <PolicyManagementPanel
            nodes={nodes}
            canManage={canManage}
            onNodesRefresh={loadNodes}
            onRequestError={handlePanelError}
          />
        ) : activeView === "system" && canManage ? (
          <SystemCheckPanel onRequestError={handlePanelError} />
        ) : (
          <>
            <section className="metric-strip node-metric-strip">
              <div className="metric-item">
                <Statistic title="节点总数" value={nodes.length} />
              </div>
              <div className="metric-item">
                <Statistic title="在线节点" value={metrics.online} />
              </div>
              <div className="metric-item">
                <Statistic title="异常/离线" value={metrics.warning} />
              </div>
              <div className="metric-item">
                <Statistic title="待更新版本" value={metrics.versionPending} />
              </div>
              <div className="metric-item">
                <Statistic title="待同步策略" value={metrics.policyPending} />
              </div>
              <div className="metric-item wide">
                <Statistic title="TCP / UDP" value={`${metrics.tcp} / ${metrics.udp}`} />
              </div>
            </section>

            <main className="workbench">
              <div className="toolbar">
                <Space wrap>
                  <Input
                    className="search-input"
                    value={query}
                    onChange={(event) => setQuery(event.target.value)}
                    onPressEnter={() => void loadNodes()}
                    prefix={<Search size={16} />}
                    placeholder="搜索节点、名称、入口"
                  />
                  <Select
                    className="filter-select"
                    allowClear
                    value={status || undefined}
                    onChange={(value) => setStatus((value ?? "") as NodeStatus | "")}
                    placeholder="状态"
                    options={[
                      { value: "new", label: "新建" },
                      { value: "deploying", label: "部署中" },
                      { value: "online", label: "在线" },
                      { value: "offline", label: "离线" },
                      { value: "error", label: "异常" },
                      { value: "disabled", label: "停用" }
                    ]}
                  />
                  <Select
                    className="filter-select"
                    allowClear
                    showSearch
                    value={region || undefined}
                    onChange={(value) => setRegion(value ?? "")}
                    placeholder="区域"
                    options={filteredRegions}
                  />
                  <Button icon={<RefreshCw size={16} />} onClick={() => void loadNodes()}>
                    刷新
                  </Button>
                </Space>
                {canManage && (
                  <Button type="primary" icon={<Plus size={16} />} onClick={openCreate}>
                    新增节点
                  </Button>
                )}
              </div>

              {error && <Alert className="inline-alert" type="error" showIcon message={error} />}
              <NodeTable
                nodes={nodes}
                loading={loading}
                canManage={canManage}
                onView={openDetail}
                onDiagnose={(node) => void openDiagnostics(node)}
                onEdit={openEdit}
                onApplyPolicy={openApplyPolicy}
                onDelete={removeNode}
              />
            </main>
          </>
        )}
      </Content>

      <NodeFormModal
        open={formOpen}
        loading={submitting}
        node={editingNode}
        onCancel={() => setFormOpen(false)}
        onSubmit={submitNode}
      />
      <SyncPolicyModal
        open={applyPolicyOpen}
        loading={applyingPolicy || policyRevisionsLoading}
        node={applyPolicyNode}
        policies={policyRevisions}
        onCancel={() => setApplyPolicyOpen(false)}
        onSubmit={submitApplyPolicy}
      />
      <DeployNodeModal
        open={deployOpen}
        loading={deploying}
        node={deployNode}
        onCancel={() => setDeployOpen(false)}
        onSubmit={submitDeploy}
      />
      <UpdateNodeModal
        open={updateOpen}
        loading={updating}
        node={targetUpdateNode}
        onCancel={() => setUpdateOpen(false)}
        onSubmit={submitUpdate}
      />
      <CredentialModal
        open={credentialOpen}
        loading={savingCredential}
        deleting={deletingCredential}
        node={credentialNode}
        credential={detailNode?.node_id === credentialNode?.node_id ? detailCredential : undefined}
        onCancel={() => setCredentialOpen(false)}
        onSubmit={submitCredential}
        onDelete={removeCredential}
      />
      <NodeDetailDrawer
        node={detailNode}
        open={detailOpen}
        reports={detailReports}
        tasks={detailTasks}
        syncStatus={detailSyncStatus}
        credential={detailCredential}
        canManage={canManage}
        loading={detailLoading}
        testingCredential={testingCredential}
        onClose={() => setDetailOpen(false)}
        onRefresh={refreshDetail}
        onOpenCredential={openCredential}
        onTestCredential={testCredential}
        onDeploy={openDeploy}
        onUpdate={openUpdate}
        onViewTaskLogs={openTaskLogs}
        onRetryTask={retryNodeTask}
        onApplyPolicy={openApplyPolicy}
      />
      <NodeDiagnosticsModal
        open={diagnosticOpen}
        loading={diagnosticsLoading}
        node={diagnosticNode}
        diagnostics={diagnostics}
        connectivityProbe={connectivityProbe}
        networkDiagnostics={networkDiagnostics}
        canManage={canManage}
        repairingAdmin={repairingAdmin}
        connectivityProbeLoading={connectivityProbeLoading}
        networkDiagnosticsLoading={networkDiagnosticsLoading}
        tuningUDPBuffer={tuningUDPBuffer}
        onRunConnectivityProbe={runConnectivityProbe}
        onRunNetworkDiagnostics={runNetworkDiagnostics}
        onRepairAdmin={repairAdminAccess}
        onTuneUDPBuffer={tuneUDPBuffer}
        onCancel={() => setDiagnosticOpen(false)}
      />
      <TaskLogDrawer
        open={taskLogOpen}
        task={taskLogTask}
        logs={taskLogs}
        loading={taskLogsLoading}
        canManage={canManage}
        onRetryTask={retryNodeTask}
        onClose={() => setTaskLogOpen(false)}
      />
      <ChangePasswordModal
        open={passwordOpen}
        loading={passwordSubmitting}
        error={passwordError}
        onCancel={() => {
          setPasswordError("");
          setPasswordOpen(false);
        }}
        onSubmit={submitChangePassword}
      />
    </Layout>
  );
}
