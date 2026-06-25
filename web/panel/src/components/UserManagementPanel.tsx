import { useCallback, useEffect, useMemo, useState } from "react";
import { Alert, Button, Form, Input, Modal, Select, Space, Table, Tag, Tooltip, Typography, message } from "antd";
import type { ColumnsType } from "antd/es/table";
import {
  Ban,
  BadgeCheck,
  Edit3,
  KeyRound,
  RefreshCw,
  Search,
  ShieldCheck,
  UserCog,
  UserPlus,
  UsersRound
} from "lucide-react";
import {
  createPanelUser,
  getSecurityOverview,
  listPanelUsers,
  PanelAPIError,
  resetPanelUserPassword,
  updatePanelUser
} from "../api";
import type {
  PanelUser,
  PanelUserCreateInput,
  PanelUserPasswordResetInput,
  PanelUserRole,
  PanelUserStatus,
  SecurityOverview,
  PanelUserUpdateInput
} from "../types";

const { Text, Title } = Typography;

type ResetPasswordForm = PanelUserPasswordResetInput & {
  confirm_password: string;
};

const roleOptions: Array<{ value: PanelUserRole; label: string }> = [
  { value: "admin", label: "管理员" },
  { value: "operator", label: "操作员" },
  { value: "viewer", label: "观察者" }
];

const statusOptions: Array<{ value: PanelUserStatus; label: string }> = [
  { value: "active", label: "启用" },
  { value: "disabled", label: "禁用" }
];

function roleLabel(role: PanelUserRole | string) {
  return roleOptions.find((item) => item.value === role)?.label ?? role;
}

function statusLabel(status: PanelUserStatus | string) {
  return statusOptions.find((item) => item.value === status)?.label ?? status;
}

function roleColor(role: PanelUserRole | string) {
  switch (role) {
    case "admin":
      return "blue";
    case "operator":
      return "green";
    case "viewer":
      return "default";
    default:
      return "default";
  }
}

function statusColor(status: PanelUserStatus | string) {
  return status === "active" ? "success" : "default";
}

function userInitials(username: string) {
  return username.trim().slice(0, 2).toUpperCase() || "U";
}

function formatDate(value: string) {
  if (!value) {
    return "-";
  }
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) {
    return value;
  }
  return date.toLocaleString();
}

function apiErrorText(error: unknown) {
  if (error instanceof PanelAPIError) {
    switch (error.code) {
      case "user_exists":
        return "账号已存在";
      case "weak_password":
        return "密码长度需要 10-128 位，且不能以前后空格开始或结束";
      case "cannot_modify_self":
        return "不能在账号管理里降级或禁用当前管理员";
      case "use_self_password_change":
        return "当前账号请使用右上角改密入口";
      case "forbidden":
        return "当前账号没有账号管理权限";
      default:
        return error.message;
    }
  }
  if (error instanceof Error) {
    return error.message;
  }
  return "请求失败";
}

function normalizeSecurityOverview(value: SecurityOverview | null | undefined): SecurityOverview | null {
  if (!value) {
    return null;
  }
  const config = value.config ?? ({} as SecurityOverview["config"]);
  return {
    users: {
      total: value.users?.total ?? 0,
      admins: value.users?.admins ?? 0,
      active: value.users?.active ?? 0,
      disabled: value.users?.disabled ?? 0
    },
    nodes: {
      total: value.nodes?.total ?? 0,
      with_credentials: value.nodes?.with_credentials ?? 0,
      without_credentials: value.nodes?.without_credentials ?? 0,
      without_hmac_secret: value.nodes?.without_hmac_secret ?? 0,
      disabled: value.nodes?.disabled ?? 0,
      offline_or_error: value.nodes?.offline_or_error ?? 0,
      policy_drift: value.nodes?.policy_drift ?? 0,
      version_drift: value.nodes?.version_drift ?? 0
    },
    config: {
      listen: config.listen ?? "",
      public_base_url: config.public_base_url ?? "",
      session_ttl_seconds: config.session_ttl_seconds ?? 0,
      backend_api_key_count: config.backend_api_key_count ?? 0,
      master_key_configured: config.master_key_configured ?? false,
      session_secret_configured: config.session_secret_configured ?? false,
      command_secret_configured: config.command_secret_configured ?? false,
      cors_allowed_origins: Array.isArray(config.cors_allowed_origins) ? config.cors_allowed_origins : []
    },
    warnings: Array.isArray(value.warnings) ? value.warnings : []
  };
}

export function UserManagementPanel({ currentUser }: { currentUser: PanelUser }) {
  const [users, setUsers] = useState<PanelUser[]>([]);
  const [security, setSecurity] = useState<SecurityOverview | null>(null);
  const [loading, setLoading] = useState(false);
  const [securityLoading, setSecurityLoading] = useState(false);
  const [error, setError] = useState("");
  const [createOpen, setCreateOpen] = useState(false);
  const [editUser, setEditUser] = useState<PanelUser | null>(null);
  const [resetUser, setResetUser] = useState<PanelUser | null>(null);
  const [submitting, setSubmitting] = useState(false);
  const [query, setQuery] = useState("");
  const [roleFilter, setRoleFilter] = useState<PanelUserRole | "">("");
  const [statusFilter, setStatusFilter] = useState<PanelUserStatus | "">("");
  const [createForm] = Form.useForm<PanelUserCreateInput>();
  const [editForm] = Form.useForm<PanelUserUpdateInput>();
  const [resetForm] = Form.useForm<ResetPasswordForm>();
  const [messageAPI, contextHolder] = message.useMessage();

  const currentIsAdmin = currentUser.role === "admin";

  const loadUsers = useCallback(async () => {
    setLoading(true);
    setError("");
    try {
      const response = await listPanelUsers();
      setUsers(Array.isArray(response.users) ? response.users : []);
    } catch (err) {
      setError(apiErrorText(err));
    } finally {
      setLoading(false);
    }
  }, []);

  const loadSecurity = useCallback(async () => {
    setSecurityLoading(true);
    try {
      const response = await getSecurityOverview();
      setSecurity(normalizeSecurityOverview(response.security));
    } catch {
      setSecurity(null);
    } finally {
      setSecurityLoading(false);
    }
  }, []);

  useEffect(() => {
    void loadUsers();
    void loadSecurity();
  }, [loadSecurity, loadUsers]);

  const activeCount = useMemo(() => users.filter((user) => user.status === "active").length, [users]);
  const adminCount = useMemo(() => users.filter((user) => user.role === "admin").length, [users]);
  const disabledCount = useMemo(() => users.filter((user) => user.status === "disabled").length, [users]);
  const filteredUsers = useMemo(() => {
    const keyword = query.trim().toLowerCase();
    return users.filter((user) => {
      const matchesQuery =
        keyword === "" ||
        user.username.toLowerCase().includes(keyword) ||
        String(user.id).includes(keyword) ||
        roleLabel(user.role).toLowerCase().includes(keyword) ||
        statusLabel(user.status).toLowerCase().includes(keyword);
      const matchesRole = roleFilter === "" || user.role === roleFilter;
      const matchesStatus = statusFilter === "" || user.status === statusFilter;
      return matchesQuery && matchesRole && matchesStatus;
    });
  }, [query, roleFilter, statusFilter, users]);

  const openCreate = () => {
    createForm.setFieldsValue({ role: "operator", status: "active" });
    setCreateOpen(true);
  };

  const openEdit = (user: PanelUser) => {
    editForm.setFieldsValue({ role: user.role, status: user.status });
    setEditUser(user);
  };

  const openReset = (user: PanelUser) => {
    resetForm.resetFields();
    setResetUser(user);
  };

  const submitCreate = async () => {
    const values = await createForm.validateFields();
    setSubmitting(true);
    try {
      await createPanelUser(values);
      messageAPI.success("账号已创建");
      setCreateOpen(false);
      createForm.resetFields();
      await loadUsers();
    } catch (err) {
      messageAPI.error(apiErrorText(err));
    } finally {
      setSubmitting(false);
    }
  };

  const submitEdit = async () => {
    if (!editUser) {
      return;
    }
    const values = await editForm.validateFields();
    setSubmitting(true);
    try {
      await updatePanelUser(editUser.id, values);
      messageAPI.success("账号权限已更新");
      setEditUser(null);
      await loadUsers();
    } catch (err) {
      messageAPI.error(apiErrorText(err));
    } finally {
      setSubmitting(false);
    }
  };

  const submitReset = async () => {
    if (!resetUser) {
      return;
    }
    const values = await resetForm.validateFields();
    setSubmitting(true);
    try {
      await resetPanelUserPassword(resetUser.id, { new_password: values.new_password });
      messageAPI.success("密码已重置");
      setResetUser(null);
      resetForm.resetFields();
    } catch (err) {
      messageAPI.error(apiErrorText(err));
    } finally {
      setSubmitting(false);
    }
  };

  const columns: ColumnsType<PanelUser> = [
    {
      title: "账号",
      dataIndex: "username",
      width: 280,
      render: (_, user) => (
        <div className="user-identity-cell">
          <span className={`user-avatar ${user.role === "admin" ? "admin" : ""}`}>{userInitials(user.username)}</span>
          <div className="user-identity-copy">
            <Space size={6} wrap>
              <strong>{user.username}</strong>
              {user.id === currentUser.id && <Tag className="soft-tag current">当前账号</Tag>}
            </Space>
            <span>ID {user.id}</span>
          </div>
        </div>
      )
    },
    {
      title: "角色",
      dataIndex: "role",
      width: 120,
      render: (value: PanelUserRole) => (
        <Tag className={`soft-tag role-${value}`} color={roleColor(value)}>
          {roleLabel(value)}
        </Tag>
      )
    },
    {
      title: "状态",
      dataIndex: "status",
      width: 120,
      render: (value: PanelUserStatus) => (
        <Tag className={`soft-tag status-${value}`} color={statusColor(value)}>
          {statusLabel(value)}
        </Tag>
      )
    },
    {
      title: "创建时间",
      dataIndex: "created_at",
      width: 190,
      render: (value: string) => <Text className="mono subtle">{formatDate(value)}</Text>
    },
    {
      title: "更新时间",
      dataIndex: "updated_at",
      width: 190,
      render: (value: string) => <Text className="mono subtle">{formatDate(value)}</Text>
    },
    {
      title: "操作",
      fixed: "right",
      width: 190,
      render: (_, user) => {
        const isSelf = user.id === currentUser.id;
        return (
          <Space size={8} className="user-actions">
            <Tooltip title={isSelf ? "当前账号保持管理员启用状态" : "编辑角色和状态"}>
              <Button
                className="text-action"
                type="text"
                icon={<Edit3 size={16} />}
                disabled={isSelf}
                onClick={() => openEdit(user)}
              >
                编辑
              </Button>
            </Tooltip>
            <Tooltip title={isSelf ? "当前账号请使用右上角改密" : "重置密码"}>
              <Button
                className="text-action"
                type="text"
                icon={<KeyRound size={16} />}
                disabled={isSelf}
                onClick={() => openReset(user)}
              >
                重置
              </Button>
            </Tooltip>
          </Space>
        );
      }
    }
  ];

  return (
    <main className="workbench users-panel">
      {contextHolder}
      <div className="users-toolbar">
        <div>
          <Text className="eyebrow">panel access</Text>
          <Title level={3}>账号与角色</Title>
          <Text type="secondary">管理员负责账号开通、禁用和角色调整；普通角色只能查看节点状态。</Text>
        </div>
        <Space wrap>
          <Button
            icon={<RefreshCw size={16} />}
            onClick={() => {
              void loadUsers();
              void loadSecurity();
            }}
            loading={loading || securityLoading}
          >
            刷新
          </Button>
          <Button type="primary" icon={<UserPlus size={16} />} onClick={openCreate} disabled={!currentIsAdmin}>
            新建账号
          </Button>
        </Space>
      </div>

      <section className="users-summary-grid">
        <div className="users-summary-item primary">
          <span className="summary-icon">
            <UsersRound size={18} />
          </span>
          <div>
            <Text>账号总数</Text>
            <strong>{users.length}</strong>
          </div>
        </div>
        <div className="users-summary-item">
          <span className="summary-icon success">
            <BadgeCheck size={18} />
          </span>
          <div>
            <Text>启用账号</Text>
            <strong>{activeCount}</strong>
          </div>
        </div>
        <div className="users-summary-item">
          <span className="summary-icon admin">
            <ShieldCheck size={18} />
          </span>
          <div>
            <Text>管理员</Text>
            <strong>{adminCount}</strong>
          </div>
        </div>
        <div className="users-summary-item">
          <span className="summary-icon muted">
            <Ban size={18} />
          </span>
          <div>
            <Text>禁用账号</Text>
            <strong>{disabledCount}</strong>
          </div>
        </div>
      </section>

      {security ? (
        <section className="security-overview">
          <div className="security-overview-head">
            <div>
              <Text className="eyebrow">安全巡检</Text>
              <strong>面板鉴权与节点运维边界</strong>
            </div>
            <Tag color={security.warnings.length ? "warning" : "success"}>
              {security.warnings.length ? `${security.warnings.length} 项提醒` : "配置正常"}
            </Tag>
          </div>
          <div className="security-overview-grid">
            <div>
              <span>Backend API Key</span>
              <strong>{security.config.backend_api_key_count} 个</strong>
            </div>
            <div>
              <span>命令签名</span>
              <strong>{security.config.command_secret_configured ? "已配置" : "未配置"}</strong>
            </div>
            <div>
              <span>会话有效期</span>
              <strong>{Math.round(security.config.session_ttl_seconds / 60)} 分钟</strong>
            </div>
            <div>
              <span>CORS 来源</span>
              <strong>{security.config.cors_allowed_origins.length || 0} 个</strong>
            </div>
            <div>
              <span>缺少 SSH 凭据</span>
              <strong>{security.nodes.without_credentials}</strong>
            </div>
            <div>
              <span>策略/版本漂移</span>
              <strong>
                {security.nodes.policy_drift} / {security.nodes.version_drift}
              </strong>
            </div>
          </div>
          {security.warnings.length ? <Alert className="inline-alert compact" type="warning" showIcon message={security.warnings.join("；")} /> : null}
        </section>
      ) : null}

      {error && <Alert className="inline-alert" type="error" showIcon message={error} />}

      <div className="users-controlbar">
        <Input
          className="users-search"
          allowClear
          value={query}
          onChange={(event) => setQuery(event.target.value)}
          prefix={<Search size={16} />}
          placeholder="搜索账号、ID、角色或状态"
        />
        <Select
          className="users-filter"
          allowClear
          value={roleFilter || undefined}
          onChange={(value) => setRoleFilter((value ?? "") as PanelUserRole | "")}
          options={roleOptions}
          placeholder="角色"
        />
        <Select
          className="users-filter"
          allowClear
          value={statusFilter || undefined}
          onChange={(value) => setStatusFilter((value ?? "") as PanelUserStatus | "")}
          options={statusOptions}
          placeholder="状态"
        />
        <span className="users-result-count">当前显示 {filteredUsers.length} 个</span>
      </div>

      <Table
        className="node-table users-table"
        columns={columns}
        dataSource={filteredUsers}
        loading={loading}
        rowKey="id"
        rowClassName={(user) => {
          const names = [];
          if (user.id === currentUser.id) {
            names.push("current-user-row");
          }
          if (user.status === "disabled") {
            names.push("disabled-user-row");
          }
          return names.join(" ");
        }}
        locale={{
          emptyText: (
            <div className="users-empty">
              <UserCog size={28} />
              <strong>没有匹配账号</strong>
              <span>调整搜索条件后再试</span>
            </div>
          )
        }}
        scroll={{ x: 1040 }}
        pagination={{
          pageSize: 10,
          showSizeChanger: false,
          showTotal: (total) => `共 ${total} 个账号`
        }}
      />

      <Modal
        title={
          <span className="modal-title-icon">
            <UserPlus size={18} />
            新建账号
          </span>
        }
        open={createOpen}
        onCancel={() => setCreateOpen(false)}
        onOk={() => void submitCreate()}
        confirmLoading={submitting}
        destroyOnClose
      >
        <Form form={createForm} layout="vertical" requiredMark={false} className="form-grid compact">
          <Form.Item name="username" label="账号" rules={[{ required: true, message: "请输入账号" }]}>
            <Input autoComplete="off" placeholder="operator01" />
          </Form.Item>
          <Form.Item
            name="password"
            label="初始密码"
            rules={[
              { required: true, message: "请输入初始密码" },
              { min: 10, message: "至少 10 位" }
            ]}
          >
            <Input.Password autoComplete="new-password" placeholder="至少 10 位" />
          </Form.Item>
          <div className="form-grid two">
            <Form.Item name="role" label="角色" rules={[{ required: true, message: "请选择角色" }]}>
              <Select options={roleOptions} />
            </Form.Item>
            <Form.Item name="status" label="状态" rules={[{ required: true, message: "请选择状态" }]}>
              <Select options={statusOptions} />
            </Form.Item>
          </div>
        </Form>
      </Modal>

      <Modal
        title={
          <span className="modal-title-icon">
            <ShieldCheck size={18} />
            编辑账号
          </span>
        }
        open={!!editUser}
        onCancel={() => setEditUser(null)}
        onOk={() => void submitEdit()}
        confirmLoading={submitting}
        destroyOnClose
      >
        <Alert
          className="inline-alert"
          type="info"
          showIcon
          message={editUser ? `正在编辑 ${editUser.username}` : ""}
        />
        <Form form={editForm} layout="vertical" requiredMark={false} className="form-grid compact">
          <div className="form-grid two">
            <Form.Item name="role" label="角色" rules={[{ required: true, message: "请选择角色" }]}>
              <Select options={roleOptions} />
            </Form.Item>
            <Form.Item name="status" label="状态" rules={[{ required: true, message: "请选择状态" }]}>
              <Select options={statusOptions} />
            </Form.Item>
          </div>
        </Form>
      </Modal>

      <Modal
        title={
          <span className="modal-title-icon">
            <KeyRound size={18} />
            重置密码
          </span>
        }
        open={!!resetUser}
        onCancel={() => setResetUser(null)}
        onOk={() => void submitReset()}
        confirmLoading={submitting}
        destroyOnClose
      >
        <Alert
          className="inline-alert"
          type="warning"
          showIcon
          message={resetUser ? `将重置 ${resetUser.username} 的登录密码` : ""}
        />
        <Form form={resetForm} layout="vertical" requiredMark={false} className="form-grid compact">
          <Form.Item
            name="new_password"
            label="新密码"
            rules={[
              { required: true, message: "请输入新密码" },
              { min: 10, message: "至少 10 位" }
            ]}
          >
            <Input.Password autoComplete="new-password" placeholder="至少 10 位" />
          </Form.Item>
          <Form.Item
            name="confirm_password"
            label="确认密码"
            dependencies={["new_password"]}
            rules={[
              { required: true, message: "请再次输入新密码" },
              ({ getFieldValue }) => ({
                validator(_, value) {
                  if (!value || getFieldValue("new_password") === value) {
                    return Promise.resolve();
                  }
                  return Promise.reject(new Error("两次密码不一致"));
                }
              })
            ]}
          >
            <Input.Password autoComplete="new-password" placeholder="再次输入新密码" />
          </Form.Item>
        </Form>
      </Modal>
    </main>
  );
}
