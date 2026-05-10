import { useEffect, useMemo, useState } from 'react'
import type { ComponentType } from 'react'
import {
  LayoutDashboard,
  Users,
  Box,
  Activity,
  Settings,
  Search,
  LogOut,
  Menu,
  ChevronDown,
  Crown,
  Wallet,
  Link,
  Trash2,
  PlusCircle,
  Gift,
  UserPlus,
  History,
  X,
  Key,
  Copy,
  Check,
} from 'lucide-react'
import { Navigate, useLocation, useNavigate } from 'react-router-dom'
import { api } from '../lib/api'
import { BRAND_LOGO_SRC } from '../lib/brand'

const MEMBERSHIP_PLAN_OPTIONS = [
  { code: 'free', label: '免费版' },
  { code: 'go', label: 'Go 版' },
  { code: 'plus', label: 'Plus 版' },
  { code: 'pro_basic', label: 'Pro 基础版' },
  { code: 'pro_max', label: 'Pro 满血版' },
]

export function AdminApp() {
  const location = useLocation()
  const navigate = useNavigate()
  const adminViews = ['dashboard', 'users', 'models', 'after-sales', 'api-stats', 'system-logs', 'member-stats', 'membership', 'finance', 'finance-management', 'settings']
  const currentView = useMemo(() => {
    const normalized = location.pathname.replace(/^\/admin\/?/, '')
    const segment = normalized.split('/')[0] || 'dashboard'
    return adminViews.includes(segment) ? segment : 'dashboard'
  }, [location.pathname])
  const [session, setSession] = useState<Awaited<ReturnType<typeof api.getSession>> | null>(null)
  const [ready, setReady] = useState(false)
  const [theme, setTheme] = useState<'dark' | 'light'>('dark')
  const [isMobileSidebarOpen, setIsMobileSidebarOpen] = useState(false)
  const [dashboard, setDashboard] = useState<any>({})
  const [users, setUsers] = useState<any[]>([])
  const [inviteItems, setInviteItems] = useState<any[]>([])
  const [models, setModels] = useState<any[]>([])
  const [serviceAlerts, setServiceAlerts] = useState<any[]>([])
  const [apiStats, setApiStats] = useState<any>({ summary: { totalRequests: 0, successRate: '0.0%', avgLatencyMs: 0, errorCount: 0 }, logs: [] })
  const [systemLogs, setSystemLogs] = useState<any[]>([])
  const [memberLogs, setMemberLogs] = useState<any[]>([])
  const [finance, setFinance] = useState<any>(null)
  const [settings, setSettings] = useState<any>(null)
  const [showModelConfig, setShowModelConfig] = useState(false)
  const [activeModel, setActiveModel] = useState<any>(null)
  const [isCreatingModel, setIsCreatingModel] = useState(false)
  const [showOAuthConfig, setShowOAuthConfig] = useState(false)
  const [activeOAuthProvider, setActiveOAuthProvider] = useState<any>(null)
  const [showMemberConfig, setShowMemberConfig] = useState(false)
  const [activeMember, setActiveMember] = useState<any>(null)
  const [showGiftModal, setShowGiftModal] = useState(false)
  const [giftStep, setGiftStep] = useState(1)
  const [giftParams, setGiftParams] = useState({ planCode: 'pro_max', duration: 1, lifetime: false, accountType: 'has_account', maxUses: 1, expiryDate: '' })
  const [giftLink, setGiftLink] = useState('')
  const [inviteLink, setInviteLink] = useState('')
  const [copiedInviteValue, setCopiedInviteValue] = useState('')
  const [copiedServiceAlertID, setCopiedServiceAlertID] = useState('')
  const [message, setMessage] = useState('')
  const [notice, setNotice] = useState<{ title: string; body: string } | null>(null)
  const [noticeExpanded, setNoticeExpanded] = useState(false)
  const [activeServiceAlert, setActiveServiceAlert] = useState<any | null>(null)
  const [serviceAlertExpanded, setServiceAlertExpanded] = useState(false)
  const [modelProbeResults, setModelProbeResults] = useState<Record<string, any>>({})
  const [activeModelProbeResult, setActiveModelProbeResult] = useState<any>(null)
  const [probingModelKey, setProbingModelKey] = useState('')

  const isDark = theme === 'dark'
  const colors = {
    appBg: isDark ? 'bg-[#111111]' : 'bg-[#f9f9f9]',
    sidebarBg: isDark ? 'bg-[#000000]' : 'bg-[#ffffff]',
    contentBg: isDark ? 'bg-[#111111]' : 'bg-[#f9f9f9]',
    cardBg: isDark ? 'bg-[#171717]' : 'bg-[#ffffff]',
    textMain: isDark ? 'text-[#ececec]' : 'text-[#111111]',
    textMuted: isDark ? 'text-[#888888]' : 'text-[#666666]',
    border: isDark ? 'border-[#333333]' : 'border-[#e5e5e5]',
    hover: isDark ? 'hover:bg-[#212121]' : 'hover:bg-[#f4f4f4]',
    inputBg: isDark ? 'bg-[#171717]' : 'bg-[#ffffff]',
    btnPrimary: isDark ? 'bg-[#ececec] text-black hover:bg-white' : 'bg-[#111111] text-white hover:bg-black',
  }
  const panelClass = `rounded-2xl border ${colors.cardBg} ${colors.border}`
  const sectionClass = `rounded-2xl border ${colors.cardBg} ${colors.border}`
  const inputClass = `w-full px-3 py-2.5 rounded-xl border text-sm ${colors.inputBg} ${colors.border}`
  const ghostButtonClass = `px-4 py-2.5 text-sm font-medium rounded-xl border ${colors.border} ${colors.hover}`
  const noticeShouldCollapse = Boolean(notice) && ((notice?.body?.length ?? 0) > 120 || /失败|异常|错误|告警|failed|error/i.test(notice?.title ?? ''))
  const serviceAlertDetail = String(activeServiceAlert?.errorDetail || '未返回错误详情')
  const serviceAlertLines = serviceAlertDetail.split(/\r?\n/)
  const serviceAlertShouldCollapse = serviceAlertDetail.length > 420 || serviceAlertLines.length > 8
  const serviceAlertPreview = (() => {
    if (!serviceAlertShouldCollapse) return serviceAlertDetail
    const preview = serviceAlertLines.slice(0, 8).join('\n')
    return `${preview.length > 520 ? `${preview.slice(0, 520).trimEnd()}...` : preview.trimEnd()}\n...`
  })()
  const recentVerificationLogs = useMemo(
    () => systemLogs.filter((log: any) => log?.eventType === 'verification_code_sent' && getSystemLogCode(log?.payload)).slice(0, 8),
    [systemLogs],
  )

  useEffect(() => {
    void (async () => {
      const currentSession = await api.getSession()
      setSession(currentSession)
      setReady(true)
      if (currentSession.admin) {
        await refresh(currentSession.admin.role)
      }
    })()
  }, [])

  useEffect(() => {
    const titleByView: Record<string, string> = {
      dashboard: 'Infinite-AI 管理后台',
      users: 'Infinite-AI 用户管理',
      models: 'Infinite-AI 模型管理',
      membership: 'Infinite-AI 会员管理',
      finance: 'Infinite-AI 财务配置',
      'finance-management': 'Infinite-AI 财务管理',
      settings: 'Infinite-AI 系统设置',
      'api-stats': 'Infinite-AI API 统计',
      'member-stats': 'Infinite-AI 会员统计',
      'after-sales': 'Infinite-AI 售后服务',
      'system-logs': 'Infinite-AI 系统日志',
    }
    document.title = titleByView[currentView] || 'Infinite-AI 管理后台'
  }, [currentView])

  useEffect(() => {
    if (!message) return
    const timer = window.setTimeout(() => setMessage(''), 3000)
    return () => window.clearTimeout(timer)
  }, [message])

  useEffect(() => {
    if (!notice) return
    setNoticeExpanded(false)
    const timer = window.setTimeout(() => setNotice(null), 3500)
    return () => window.clearTimeout(timer)
  }, [notice])

  useEffect(() => {
    setServiceAlertExpanded(false)
  }, [activeServiceAlert?.id])

  useEffect(() => {
    if (!session?.admin) return
    const adminRole = session.admin.role
    const timer = window.setInterval(() => {
      void (async () => {
        try {
          const nextServiceAlertsPromise = canAccessAdminSection(adminRole, 'service-alerts')
            ? api.adminServiceAlerts()
            : Promise.resolve({ alerts: [] })
          const [nextApiStats, nextServiceAlerts] = await Promise.all([
            api.adminApiStats(),
            nextServiceAlertsPromise,
          ])
          setApiStats(nextApiStats)
          setServiceAlerts(nextServiceAlerts.alerts ?? [])
          if (currentView === 'system-logs' && canAccessAdminSection(adminRole, 'system-logs')) {
            const nextSystemLogs = await api.adminSystemLogs()
            setSystemLogs(nextSystemLogs.logs ?? [])
          }
          surfaceOpsAlert(nextServiceAlerts.alerts ?? [])
        } catch {
          // Best-effort background refresh; the foreground UI will surface errors.
        }
      })()
    }, 12000)
    return () => window.clearInterval(timer)
  }, [currentView, session?.admin])

  async function refresh(roleOverride?: string) {
    const adminRole = roleOverride || session?.admin?.role || ''
    const [dashboardResponse, usersResponse, inviteResponse, modelsResponse, serviceAlertsResponse, apiStatsResponse, systemLogsResponse, memberResponse, financeResponse, settingsResponse] = await Promise.allSettled([
      api.adminDashboard(),
      api.adminUsers(),
      api.adminInviteLinks(),
      api.adminModels(),
      canAccessAdminSection(adminRole, 'service-alerts') ? api.adminServiceAlerts() : Promise.resolve({ alerts: [] }),
      api.adminApiStats(),
      canAccessAdminSection(adminRole, 'system-logs') ? api.adminSystemLogs() : Promise.resolve({ logs: [] }),
      api.adminMemberStats(),
      api.adminFinance(),
      api.adminSettings(),
    ])
    setDashboard(dashboardResponse.status === 'fulfilled' ? dashboardResponse.value ?? { stats: [] } : { stats: [] })
    setUsers(usersResponse.status === 'fulfilled' ? usersResponse.value.users ?? [] : [])
    setInviteItems(inviteResponse.status === 'fulfilled' ? inviteResponse.value.invites ?? [] : [])
    setModels(modelsResponse.status === 'fulfilled' ? modelsResponse.value.models ?? [] : [])
    const nextServiceAlerts = serviceAlertsResponse.status === 'fulfilled' ? serviceAlertsResponse.value?.alerts ?? [] : []
    setServiceAlerts(nextServiceAlerts)
    const nextApiStats = apiStatsResponse.status === 'fulfilled'
      ? apiStatsResponse.value ?? { summary: { totalRequests: 0, successRate: '0.0%', avgLatencyMs: 0, errorCount: 0 }, logs: [] }
      : { summary: { totalRequests: 0, successRate: '0.0%', avgLatencyMs: 0, errorCount: 0 }, logs: [] }
    setApiStats(nextApiStats)
    setSystemLogs(systemLogsResponse.status === 'fulfilled' ? systemLogsResponse.value.logs ?? [] : [])
    surfaceOpsAlert(nextServiceAlerts)
    setMemberLogs(memberResponse.status === 'fulfilled' ? memberResponse.value.logs ?? [] : [])
    setFinance(financeResponse.status === 'fulfilled' ? financeResponse.value ?? { ifpayConfig: {}, transactions: [] } : { ifpayConfig: {}, transactions: [] })
    setSettings(
      settingsResponse.status === 'fulfilled'
        ? settingsResponse.value ?? { oauthProviders: [], registerEnabled: false, authSecurity: {}, emailGateway: {}, smsGateway: {}, modelMembershipLimits: {}, modelContextLimits: { default: 0, models: {}, plans: {}, users: {} }, infiniteCodeQuotaConfig: {}, shareCollaborationConfig: {}, searchProvider: {} }
        : { oauthProviders: [], registerEnabled: false, authSecurity: {}, emailGateway: {}, smsGateway: {}, modelMembershipLimits: {}, modelContextLimits: { default: 0, models: {}, plans: {}, users: {} }, infiniteCodeQuotaConfig: {}, shareCollaborationConfig: {}, searchProvider: {} },
    )
    const failedSections = [
      { name: 'dashboard', result: dashboardResponse },
      { name: 'users', result: usersResponse },
      { name: 'invite-links', result: inviteResponse },
      { name: 'models', result: modelsResponse },
      { name: 'service-alerts', result: serviceAlertsResponse },
      { name: 'api-stats', result: apiStatsResponse },
      { name: 'system-logs', result: systemLogsResponse },
      { name: 'member-stats', result: memberResponse },
      { name: 'finance', result: financeResponse },
      { name: 'settings', result: settingsResponse },
    ].filter((item) => item.result.status === 'rejected' && !isIgnorableAdminLoadError((item.result as PromiseRejectedResult).reason)).map((item) => item.name)
    if (failedSections.length > 0) {
      setMessage(`部分后台数据加载失败：${failedSections.map(formatAdminSectionName).join('、')}`)
    }
  }

  function surfaceOpsAlert(alerts: any[]) {
    if (!Array.isArray(alerts)) return
    const nextUnread = alerts.find((item) => item?.status === 'unread')
    if (!nextUnread) {
      setActiveServiceAlert(null)
      return
    }
    setActiveServiceAlert((current: any | null) => {
      if (current?.id === nextUnread.id) {
        return current
      }
      return nextUnread
    })
  }

  async function handleReadServiceAlert(alertId: string) {
    try {
      await api.adminReadServiceAlert(alertId)
      setActiveServiceAlert(null)
      await refresh()
    } catch (error) {
      setNotice({
        title: '告警已读失败',
        body: error instanceof Error ? error.message : '告警状态更新失败，请稍后再试。',
      })
    }
  }

  async function handleResolveServiceAlert(alertId: string) {
    try {
      await api.adminResolveServiceAlert(alertId)
      if (activeServiceAlert?.id === alertId) {
        setActiveServiceAlert(null)
      }
      setNotice({
        title: '告警已处理',
        body: '该异常记录已从售后服务待处理列表中移除。',
      })
      await refresh()
    } catch (error) {
      setNotice({
        title: '告警处理失败',
        body: error instanceof Error ? error.message : '异常处理状态更新失败，请稍后再试。',
      })
    }
  }

  async function handleLogout() {
    await api.adminLogout()
    navigate('/admin/login', { replace: true })
  }

  async function saveModelConfig() {
    if (!activeModel) return
    try {
      const payload = {
        ...activeModel,
        slug: String(activeModel.slug ?? '').trim(),
        upstreamModel: String(activeModel.upstreamModel ?? '').trim(),
        endpoints: Array.isArray(activeModel.endpoints) ? activeModel.endpoints.filter((endpoint: any) => endpoint.baseUrl?.trim()) : [],
      }
      if (!payload.slug) {
        throw new Error('模型 slug 不能为空')
      }
      if (!payload.upstreamModel) {
        throw new Error('上游模型名不能为空')
      }
      if (payload.endpoints.length === 0) {
        throw new Error('至少需要配置一个可用端点')
      }
      if (isCreatingModel) {
        await api.adminCreateModel(payload)
      } else {
        await api.adminUpdateModel(payload.slug, payload)
      }
      setMessage(isCreatingModel ? '新模型已创建' : '模型配置已保存')
      setNotice({
        title: isCreatingModel ? '模型已创建' : '模型已保存',
        body: `${payload.name || payload.slug} 的配置已经写入后端并立即生效。`,
      })
      setShowModelConfig(false)
      setIsCreatingModel(false)
      await refresh()
    } catch (error) {
      setNotice({
        title: '模型保存失败',
        body: error instanceof Error ? error.message : '模型配置保存失败，请检查必填项和端点配置。',
      })
    }
  }

  async function handleDeleteModel(model: any) {
    const modelName = model?.name || model?.slug || '该模型'
    if (!window.confirm(`确认删除 ${modelName} 吗？删除后该模型会立刻从用户端和后台配置中移除。`)) {
      return
    }
    try {
      await api.adminDeleteModel(String(model.slug || ''))
      if (activeModel?.slug === model?.slug) {
        setShowModelConfig(false)
        setActiveModel(null)
      }
      setNotice({
        title: '模型已删除',
        body: `${modelName} 已从模型管理中移除。`,
      })
      await refresh()
    } catch (error) {
      setNotice({
        title: '模型删除失败',
        body: error instanceof Error ? error.message : '模型删除失败，请稍后再试。',
      })
    }
  }

  async function saveOAuthProvider() {
    if (!activeOAuthProvider) return
    const payload = {
      ...activeOAuthProvider,
      authParams: parseLooseJSON(activeOAuthProvider.authParamsText),
      tokenParams: parseLooseJSON(activeOAuthProvider.tokenParamsText),
    }
    if (activeOAuthProvider.isNew) {
      await api.adminCreateOAuth(payload)
      setMessage('OAuth Provider 已创建')
    } else {
      await api.adminUpdateOAuth(activeOAuthProvider.slug, payload)
      setMessage(`已保存 ${activeOAuthProvider.name}`)
    }
    setShowOAuthConfig(false)
    setActiveOAuthProvider(null)
    await refresh()
  }

  async function saveMemberConfig() {
    if (!activeMember) return
    try {
      await api.adminUpdateUser(activeMember.id, {
        planCode: activeMember.rawPlan,
        expiryDate: activeMember.expiryDate,
        status: activeMember.statusCode || 'active',
      })
      if (activeMember.contextLimits && typeof activeMember.contextLimits === 'object') {
        const nextContextConfig = {
          default: Number(settings?.modelContextLimits?.default ?? 0) || 0,
          models: { ...(settings?.modelContextLimits?.models ?? {}) },
          plans: { ...(settings?.modelContextLimits?.plans ?? {}) },
          users: {
            ...(settings?.modelContextLimits?.users ?? {}),
            [activeMember.id]: activeMember.contextLimits,
          },
        }
        await api.adminUpdateModelContextLimits(normalizeModelContextLimitPayload(nextContextConfig))
      }
      setShowMemberConfig(false)
      setNotice({
        title: '会员配置已保存',
        body: `${activeMember.name} 的套餐和状态已经更新。`,
      })
      await refresh()
    } catch (error) {
      setNotice({
        title: '会员配置保存失败',
        body: error instanceof Error ? error.message : '会员配置保存失败，请稍后再试。',
      })
    }
  }

  async function handleQuickMembershipChange(user: any, direction: 'upgrade' | 'downgrade') {
    const nextPlan = getAdjacentPlan(user.rawPlan, direction)
    if (!nextPlan) {
      setNotice({
        title: direction === 'upgrade' ? '已经是最高档套餐' : '已经是最低档套餐',
        body: `${user.name} 当前没有可继续${direction === 'upgrade' ? '升级' : '降级'}的档位。`,
      })
      return
    }
    try {
      await api.adminUpdateUser(user.id, {
        planCode: nextPlan.code,
        expiryDate: '',
        status: user.statusCode || 'active',
      })
      setNotice({
        title: direction === 'upgrade' ? '会员已升级' : '会员已降级',
        body: `${user.name} 已切换到 ${nextPlan.label}。`,
      })
      await refresh()
    } catch (error) {
      setNotice({
        title: direction === 'upgrade' ? '升级失败' : '降级失败',
        body: error instanceof Error ? error.message : '会员变更失败，请稍后再试。',
      })
    }
  }

  async function handleUserStatusChange(user: any, nextStatus: 'active' | 'banned') {
    try {
      await api.adminUpdateUser(user.id, {
        planCode: '',
        expiryDate: '',
        status: nextStatus,
      })
      setNotice({
        title: nextStatus === 'active' ? '账号已解封' : '账号已封号',
        body: `${user.name} 当前状态已更新为${formatUserStatus(nextStatus)}。`,
      })
      if (activeMember?.id === user.id) {
        setActiveMember((prev: any) => prev ? { ...prev, statusCode: nextStatus, status: formatUserStatus(nextStatus) } : prev)
      }
      await refresh()
    } catch (error) {
      setNotice({
        title: nextStatus === 'active' ? '解封失败' : '封号失败',
        body: error instanceof Error ? error.message : '用户状态更新失败，请稍后再试。',
      })
    }
  }

  function openMemberConfig(user: any) {
    setActiveMember({
      ...user,
      expiryDate: '',
      statusCode: user.statusCode || 'active',
      contextLimits: { ...(settings?.modelContextLimits?.users?.[user.id] ?? {}) },
    })
    setShowMemberConfig(true)
  }

  async function handleGenerateGift() {
    try {
      const result = await api.adminCreateRedeemCampaign({
        name: 'Admin Gift',
        planCode: giftParams.planCode,
        duration: giftParams.duration,
        lifetime: giftParams.lifetime,
        accountType: giftParams.accountType,
        maxUses: giftParams.maxUses,
        expiryDate: giftParams.expiryDate,
      })
      setGiftLink(result.link)
      setGiftStep(2)
      setNotice({
        title: '兑换链接已生成',
        body: '礼品卡兑换链接已经创建成功，现在可以直接打开或复制分享。',
      })
    } catch (error) {
      setNotice({
        title: '兑换链接生成失败',
        body: error instanceof Error ? error.message : '生成兑换链接失败，请稍后重试。',
      })
    }
  }

  async function handleCreateInvite() {
    const result = await api.adminCreateInvite()
    setInviteLink(result.link)
    setMessage('邀请链接已生成')
    await refresh()
  }

  async function handleRevokeInvite(code: string) {
    try {
      await api.adminRevokeInvite(code)
      setInviteItems((prev) => prev.filter((invite) => invite.code !== code))
      setNotice({
        title: '邀请已撤回',
        body: '该邀请链接已经从最近邀请记录中删除，后续无法继续使用。',
      })
    } catch (error) {
      setNotice({
        title: '撤回邀请失败',
        body: error instanceof Error ? error.message : '邀请撤回失败，请稍后再试。',
      })
    }
  }

  async function saveModelMembershipLimits() {
    try {
      await api.adminUpdateModelMembershipLimits(normalizeModelMembershipLimitPayload(settings?.modelMembershipLimits))
      setNotice({
        title: '模型套餐权限已保存',
        body: '各套餐可用模型与 24 小时回复次数限制已经生效。',
      })
      await refresh()
    } catch (error) {
      setNotice({
        title: '模型套餐权限保存失败',
        body: error instanceof Error ? error.message : '模型套餐权限保存失败，请检查输入后重试。',
      })
    }
  }

  async function saveModelContextLimits() {
    try {
      await api.adminUpdateModelContextLimits(normalizeModelContextLimitPayload(settings?.modelContextLimits))
      setNotice({
        title: '上下文长度已保存',
        body: '不同套餐、不同模型的上下文输入长度限制已经生效。限制只裁剪输入历史，不会把模型正在输出的文章截断。',
      })
      await refresh()
    } catch (error) {
      setNotice({
        title: '上下文长度保存失败',
        body: error instanceof Error ? error.message : '上下文长度保存失败，请检查输入后重试。',
      })
    }
  }

  function getModelPlanLimit(planCode: string, modelSlug: string) {
    return settings?.modelMembershipLimits?.[planCode]?.[modelSlug]
  }

  function getModelPlanContextLimit(planCode: string, modelSlug: string) {
    return settings?.modelContextLimits?.plans?.[planCode]?.[modelSlug]
  }

  function getModelDefaultContextLimit(modelSlug: string) {
    return settings?.modelContextLimits?.models?.[modelSlug]
  }

  function setModelPlanAvailability(planCode: string, modelSlug: string, enabled: boolean) {
    setSettings((prev: any) => {
      const nextLimits = { ...(prev?.modelMembershipLimits ?? {}) }
      const planLimits = { ...(nextLimits[planCode] ?? {}) }
      if (enabled) {
        delete planLimits[modelSlug]
      } else {
        planLimits[modelSlug] = 0
      }
      nextLimits[planCode] = planLimits
      return {
        ...(prev ?? {}),
        modelMembershipLimits: nextLimits,
      }
    })
  }

  function setModelPlanQuota(planCode: string, modelSlug: string, value: string) {
    setSettings((prev: any) => {
      const nextLimits = { ...(prev?.modelMembershipLimits ?? {}) }
      const planLimits = { ...(nextLimits[planCode] ?? {}) }
      const trimmed = value.trim()
      if (trimmed === '') {
        delete planLimits[modelSlug]
      } else {
        planLimits[modelSlug] = trimmed
      }
      nextLimits[planCode] = planLimits
      return {
        ...(prev ?? {}),
        modelMembershipLimits: nextLimits,
      }
    })
  }

  function setModelPlanContextLimit(planCode: string, modelSlug: string, value: string) {
    setSettings((prev: any) => {
      const nextConfig = {
        default: Number(prev?.modelContextLimits?.default ?? 0) || 0,
        models: { ...(prev?.modelContextLimits?.models ?? {}) },
        plans: { ...(prev?.modelContextLimits?.plans ?? {}) },
        users: { ...(prev?.modelContextLimits?.users ?? {}) },
      }
      const planLimits = { ...(nextConfig.plans[planCode] ?? {}) }
      const trimmed = value.trim()
      if (trimmed === '') {
        delete planLimits[modelSlug]
      } else {
        planLimits[modelSlug] = trimmed
      }
      nextConfig.plans[planCode] = planLimits
      return {
        ...(prev ?? {}),
        modelContextLimits: nextConfig,
      }
    })
  }

  function setModelDefaultContextLimit(modelSlug: string, value: string) {
    setSettings((prev: any) => {
      const nextConfig = {
        default: Number(prev?.modelContextLimits?.default ?? 0) || 0,
        models: { ...(prev?.modelContextLimits?.models ?? {}) },
        plans: { ...(prev?.modelContextLimits?.plans ?? {}) },
        users: { ...(prev?.modelContextLimits?.users ?? {}) },
      }
      const trimmed = value.trim()
      if (trimmed === '') {
        delete nextConfig.models[modelSlug]
      } else {
        nextConfig.models[modelSlug] = trimmed
      }
      return {
        ...(prev ?? {}),
        modelContextLimits: nextConfig,
      }
    })
  }

  async function saveInfiniteCodeQuotaConfig() {
    try {
      await api.adminUpdateInfiniteCodeQuotaConfig(normalizeInfiniteCodeQuotaPayload(settings?.infiniteCodeQuotaConfig))
      setNotice({
        title: 'Infinite Code 配额已保存',
        body: '各套餐的编程助手周期额度和刷新时间已经生效。',
      })
      await refresh()
    } catch (error) {
      setNotice({
        title: 'Infinite Code 配额保存失败',
        body: error instanceof Error ? error.message : 'Infinite Code 配额保存失败，请检查输入后重试。',
      })
    }
  }

  async function saveShareCollaborationConfig() {
    try {
      await api.adminUpdateShareCollaborationConfig(normalizeShareCollaborationPayload(settings?.shareCollaborationConfig))
      setNotice({
        title: '对话协作额度已保存',
        body: '各套餐的分享协作人数上限已经更新。',
      })
      await refresh()
    } catch (error) {
      setNotice({
        title: '对话协作额度保存失败',
        body: error instanceof Error ? error.message : '对话协作额度保存失败，请检查输入后重试。',
      })
    }
  }

  async function handleProbeModel(model: any, options?: { fromModal?: boolean }) {
    const key = modelProbeKey(model)
    setProbingModelKey(key)
    try {
      const result = await api.adminTestModelRoute({
        ...model,
        endpoints: Array.isArray(model?.endpoints) ? model.endpoints : [],
      })
      setModelProbeResults((prev) => ({ ...prev, [key]: result }))
      if (options?.fromModal) {
        setActiveModelProbeResult(result)
      }
      setNotice({
        title: '模型通路测试完成',
        body: `${model.name || model.slug || '当前模型'} 当前成功率 ${result?.summary?.successRate ?? 0}%，${result?.summary?.message || '测试已完成。'}`,
      })
    } catch (error) {
      const fallback = {
        summary: {
          status: 'failed',
          successRate: 0,
          successfulEndpoints: 0,
          activeEndpoints: countActiveEndpoints(model),
          totalEndpoints: Array.isArray(model?.endpoints) ? model.endpoints.length : 0,
          avgLatencyMs: 0,
          message: error instanceof Error ? error.message : '测试失败',
        },
        endpoints: [],
      }
      setModelProbeResults((prev) => ({ ...prev, [key]: fallback }))
      if (options?.fromModal) {
        setActiveModelProbeResult(fallback)
      }
      setNotice({
        title: '模型通路测试失败',
        body: error instanceof Error ? error.message : '测试失败，请检查模型配置后重试。',
      })
    } finally {
      setProbingModelKey('')
    }
  }

  async function copyInviteValue(value: string) {
    await navigator.clipboard.writeText(value)
    setCopiedInviteValue(value)
    setTimeout(() => setCopiedInviteValue((current) => (current === value ? '' : current)), 1500)
  }

  async function handleCopyServiceAlert(alert: any) {
    const value = formatServiceAlertCopyText(alert)
    await navigator.clipboard.writeText(value)
    setCopiedServiceAlertID(String(alert?.id ?? ''))
    setNotice({
      title: '完整报错已复制',
      body: '已复制包含账号、模型、时间、来源、路径和完整错误详情的异常信息。',
    })
    window.setTimeout(() => {
      setCopiedServiceAlertID((current) => (current === String(alert?.id ?? '') ? '' : current))
    }, 1500)
  }

  function openOAuthEditor(provider?: any) {
    const nextProvider = provider
      ? {
          ...provider,
          authParamsText: JSON.stringify(provider.authParams ?? {}, null, 2),
          tokenParamsText: JSON.stringify(provider.tokenParams ?? {}, null, 2),
          isNew: false,
        }
      : {
          slug: '',
          name: '',
          providerKind: 'oauth2',
          enabled: false,
          logoUrl: '',
          authUrl: '',
          tokenUrl: '',
          userInfoUrl: '',
          scopes: 'openid email profile',
          clientId: '',
          clientSecret: '',
          userIdField: 'id',
          userEmailField: 'email',
          userNameField: 'name',
          authParamsText: '{}',
          tokenParamsText: '{\n  "grant_type": "authorization_code"\n}',
          isNew: true,
        }
    setActiveOAuthProvider(nextProvider)
    setShowOAuthConfig(true)
  }

  function openModelEditor(model?: any) {
    if (model) {
      setActiveModel({ ...JSON.parse(JSON.stringify(model)), endpoints: Array.isArray(model.endpoints) ? model.endpoints : [] })
      setIsCreatingModel(false)
      setActiveModelProbeResult(modelProbeResults[modelProbeKey(model)] ?? null)
    } else {
      setActiveModel({
        slug: '',
        name: '',
        protocol: 'openai',
        strategy: 'sequential',
        modelType: 'chat',
        upstreamModel: '',
        description: '',
        promptEnabled: false,
        promptText: '',
        active: true,
        endpoints: [{ baseUrl: '', secret: '', active: true }],
      })
      setIsCreatingModel(true)
      setActiveModelProbeResult(null)
    }
    setShowModelConfig(true)
  }

  function openPhotoModelEditor() {
    const current = models.find((model) => model.modelType === 'image')
    if (current) {
      openModelEditor(current)
      return
    }
    setActiveModel({
      slug: 'infinite-ai-photo',
      name: 'Infinite-AI Photo',
      protocol: 'openai',
      strategy: 'sequential',
      modelType: 'image',
      upstreamModel: 'gpt-image-2',
      description: '独立于对话模型的照片生成配置，供 /v1/images/generations 调用。',
      promptEnabled: false,
      promptText: '',
      active: true,
      endpoints: [{ baseUrl: '', secret: '', active: true }],
    })
    setIsCreatingModel(true)
    setActiveModelProbeResult(null)
    setShowModelConfig(true)
  }

  async function handleOAuthLogoUpload(file: File | null) {
    if (!file) return
    const dataUrl = await readFileAsDataURL(file)
    setActiveOAuthProvider((prev: any) => (prev ? { ...prev, logoUrl: dataUrl } : prev))
  }

  if (!ready) {
    return <div className="min-h-screen bg-black text-white flex items-center justify-center">正在加载...</div>
  }

  if (!session?.admin) {
    return <Navigate to="/admin/login" replace />
  }

  const titleMap: Record<string, string> = {
    dashboard: '数据大盘',
    users: '用户管理',
    models: '模型管理',
    'after-sales': '售后服务',
    'api-stats': 'API 监控',
    'system-logs': '系统日志',
    'member-stats': '会员监控',
    membership: '会员管理',
    finance: '财务配置',
    'finance-management': '财务管理',
    settings: '全局设置',
  }
  const chatModels = models.filter((model) => model.modelType !== 'image')
  const photoModel = models.find((model) => model.modelType === 'image') ?? null
  const photoProbeResult = photoModel ? modelProbeResults[modelProbeKey(photoModel)] ?? null : null
  const pendingServiceAlerts = serviceAlerts.filter((alert) => alert.status !== 'resolved')
  const unreadServiceAlerts = pendingServiceAlerts.filter((alert) => alert.status === 'unread')

  return (
    <div className={`flex h-screen overflow-hidden ${colors.appBg} ${colors.textMain}`}>
      {isMobileSidebarOpen && <div className="fixed inset-0 bg-black/60 z-40 md:hidden" onClick={() => setIsMobileSidebarOpen(false)} />}
      <aside className={`fixed md:relative z-50 h-full flex flex-col transition-all duration-300 w-[260px] border-r ${colors.sidebarBg} ${colors.border} ${isMobileSidebarOpen ? 'translate-x-0' : '-translate-x-full md:translate-x-0'}`}>
        <div className={`h-16 flex items-center px-6 border-b ${colors.border} shrink-0`}>
          <div className="flex items-center gap-2 font-medium tracking-wide">
            <img src={BRAND_LOGO_SRC} alt="Infinite-AI 管理后台" className="h-8 w-8 rounded-xl object-cover" />
            <span>Infinite-AI 管理后台</span>
          </div>
        </div>
        <div className="flex-1 overflow-y-auto py-6 px-4 space-y-1">
          <div className={`text-xs font-semibold px-2 mb-3 mt-2 ${colors.textMuted} tracking-wider uppercase`}>系统概览</div>
          {([
            ['dashboard', LayoutDashboard, '数据大盘'],
            ['users', Users, '用户管理'],
            ['models', Box, '模型管理'],
            ['after-sales', History, '售后服务'],
            ['api-stats', Activity, 'API 监控'],
            ['system-logs', History, '系统日志'],
            ['member-stats', History, '会员监控'],
            ['membership', Crown, '会员管理'],
            ['finance-management', Wallet, '财务管理'],
            ['finance', Wallet, '财务配置'],
            ['settings', Settings, '全局设置'],
          ] as Array<[string, ComponentType<{ className?: string }>, string]>).map(([key, Icon, label]) => (
            <button key={key} onClick={() => navigate(`/admin/${key}`)} className={`w-full flex items-center gap-3 px-3 py-2.5 rounded-lg text-sm font-medium ${currentView === key ? (isDark ? 'bg-[#212121] text-white' : 'bg-[#f4f4f4] text-black') : colors.hover}`}>
              <Icon className="w-4 h-4" /> <span>{label}</span>
            </button>
          ))}
        </div>
        <div className={`p-4 border-t ${colors.border}`}>
          <button onClick={handleLogout} className={`w-full flex items-center gap-3 px-2 py-2 rounded-lg ${colors.hover}`}>
            <div className="w-8 h-8 rounded-full bg-blue-600 flex items-center justify-center text-white text-xs font-bold">AD</div>
            <div className="flex-1 truncate text-left">
              <div className="text-sm font-medium">{session.admin.displayName}</div>
              <div className={`text-xs ${colors.textMuted}`}>{formatAdminRole(session.admin.role)}</div>
            </div>
            <LogOut className={`w-4 h-4 ${colors.textMuted}`} />
          </button>
        </div>
      </aside>
      <main className={`flex-1 flex flex-col h-screen overflow-hidden ${colors.contentBg}`}>
        <header className={`h-16 flex items-center justify-between px-6 border-b shrink-0 ${colors.sidebarBg} ${colors.border}`}>
          <div className="flex items-center gap-4">
            <button className={`md:hidden p-2 -ml-2 rounded-md ${colors.textMuted} ${colors.hover}`} onClick={() => setIsMobileSidebarOpen(true)}>
              <Menu className="w-5 h-5" />
            </button>
            <h1 className="text-lg font-medium capitalize">{titleMap[currentView] ?? currentView}</h1>
          </div>
          <div className="text-sm text-gray-400">{message}</div>
        </header>
        <div className="flex-1 overflow-y-auto p-6">
          {currentView === 'dashboard' && (
            <div className="space-y-6">
              <div className={`${panelClass} p-6`}>
                <h2 className="text-2xl font-semibold">平台总览</h2>
                <p className={`mt-2 text-sm ${colors.textMuted}`}>这里会实时汇总用户、订阅、API 和延迟表现。直接打开 `/admin` 也会自动进入这个大盘。</p>
              </div>
              {(dashboard.stats ?? []).length > 0 ? (
                <div className="grid grid-cols-1 md:grid-cols-2 xl:grid-cols-4 gap-6">
                  {(dashboard.stats ?? []).map((stat: any) => (
                    <div key={stat.label} className={`p-6 rounded-xl border ${colors.cardBg} ${colors.border}`}>
                      <div className={`text-sm font-medium mb-3 ${colors.textMuted}`}>{stat.label}</div>
                      <div className="text-3xl font-semibold mb-2">{stat.value}</div>
                      <div className="text-sm text-green-500">{stat.trend}</div>
                    </div>
                  ))}
                </div>
              ) : (
                <div className={`${panelClass} p-10 text-center`}>
                  <div className="text-lg font-medium">大盘数据还没有加载出来</div>
                  <p className={`mt-2 text-sm ${colors.textMuted}`}>如果你是直接从 `/admin` 进入，刷新一次现在会自动跳到 `/admin/dashboard`。如果仍为空，我再继续把统计项补深一些。</p>
                </div>
              )}
            </div>
          )}

          {currentView === 'users' && (
            <div className="space-y-6">
              <div className="flex justify-between items-center">
                <div>
                  <h2 className="text-2xl font-medium">用户管理</h2>
                  <p className={`text-sm mt-1 ${colors.textMuted}`}>管理系统内的所有注册用户、订阅套餐与状态。</p>
                </div>
                <button onClick={handleCreateInvite} className={`px-4 py-2 text-sm font-medium rounded-md border flex items-center gap-2 ${colors.border} ${colors.hover}`}>
                  <UserPlus className="w-4 h-4" /> 邀请用户 (加 Aff)
                </button>
              </div>
              {inviteLink && (
                <div className={`${panelClass} p-4 flex flex-col gap-3 md:flex-row md:items-center md:justify-between`}>
                  <div>
                    <div className="text-sm font-medium">最新邀请链接已生成</div>
                    <div className={`text-sm break-all mt-1 ${colors.textMuted}`}>{inviteLink}</div>
                  </div>
                  <button onClick={() => void copyInviteValue(inviteLink)} className={ghostButtonClass}>
                    {copiedInviteValue === inviteLink ? <Check className="w-4 h-4 inline mr-2" /> : <Copy className="w-4 h-4 inline mr-2" />}
                    {copiedInviteValue === inviteLink ? '已复制' : '复制链接'}
                  </button>
                </div>
              )}
              <div className={`${panelClass} overflow-hidden`}>
                <div className={`p-4 border-b flex items-center justify-between ${colors.border}`}>
                  <div>
                    <div className="text-sm font-medium">最近邀请记录</div>
                    <div className={`text-xs mt-1 ${colors.textMuted}`}>这里能直接看到邀请码是否已经完成注册归因。</div>
                  </div>
                </div>
                <div className="overflow-x-auto">
                  <table className="w-full text-left min-w-[860px]">
                    <thead>
                      <tr className={`border-b text-xs uppercase tracking-wider ${colors.border} ${colors.textMuted}`}>
                        <th className="px-4 py-3 font-medium">邀请码</th>
                        <th className="px-4 py-3 font-medium">邀请链接</th>
                        <th className="px-4 py-3 font-medium">状态</th>
                        <th className="px-4 py-3 font-medium">创建时间</th>
                        <th className="px-4 py-3 font-medium">使用用户</th>
                        <th className="px-4 py-3 font-medium text-right">操作</th>
                      </tr>
                    </thead>
                    <tbody>
                      {inviteItems.map((invite) => (
                        <tr key={invite.code} className={colors.hover}>
                          <td className="px-4 py-3 font-mono text-sm">{invite.code}</td>
                          <td className={`px-4 py-3 text-sm max-w-[320px] truncate ${colors.textMuted}`}>{invite.link}</td>
                          <td className="px-4 py-3">
                            <span className={`inline-flex rounded-full px-2.5 py-1 text-xs font-medium border ${invite.status === 'consumed' ? 'border-green-500/30 text-green-500 bg-green-500/10' : 'border-amber-500/30 text-amber-500 bg-amber-500/10'}`}>
                              {invite.status === 'consumed' ? '已注册' : '待使用'}
                            </span>
                          </td>
                          <td className="px-4 py-3 text-sm">{invite.createdAt}</td>
                          <td className="px-4 py-3 text-sm">{invite.consumedBy || '-'}</td>
	                          <td className="px-4 py-3 text-right">
	                            <div className="flex items-center justify-end gap-2">
	                              <button onClick={() => void copyInviteValue(invite.link)} className={`px-3 py-1.5 rounded-md text-xs font-medium border ${colors.border} ${colors.hover}`}>
	                                {copiedInviteValue === invite.link ? '已复制' : '复制'}
	                              </button>
	                              <button onClick={() => void handleRevokeInvite(invite.code)} className="px-3 py-1.5 rounded-md text-xs font-medium border border-red-500/30 text-red-500 hover:bg-red-500/10">
	                                撤回
	                              </button>
	                            </div>
	                          </td>
                        </tr>
                      ))}
                      {inviteItems.length === 0 && (
                        <tr>
                          <td colSpan={6} className={`px-4 py-8 text-center text-sm ${colors.textMuted}`}>还没有生成过邀请链接</td>
                        </tr>
                      )}
                    </tbody>
                  </table>
                </div>
              </div>
              <div className={`rounded-xl border overflow-hidden ${colors.cardBg} ${colors.border}`}>
                <div className={`p-4 border-b flex gap-4 ${colors.border}`}>
                  <div className={`flex items-center px-3 py-1.5 rounded-md border w-full max-w-sm ${colors.inputBg} ${colors.border}`}>
                    <Search className={`w-4 h-4 ${colors.textMuted}`} />
                    <input type="text" placeholder="通过邮箱或 ID 搜索..." className={`bg-transparent border-none outline-none text-sm ml-2 w-full ${colors.textMain}`} />
                  </div>
                </div>
                <div className="overflow-x-auto">
                  <table className="w-full text-left border-collapse min-w-[800px]">
                    <thead>
                      <tr className={`border-b text-xs uppercase tracking-wider ${colors.border} ${colors.textMuted}`}>
                        <th className="px-6 py-4 font-medium">用户 ID</th>
                        <th className="px-6 py-4 font-medium">姓名 & 邮箱</th>
                        <th className="px-6 py-4 font-medium">订阅套餐</th>
                        <th className="px-6 py-4 font-medium">Tokens 消耗</th>
                        <th className="px-6 py-4 font-medium">状态</th>
                        <th className="px-6 py-4 font-medium text-right">操作</th>
                      </tr>
                    </thead>
                    <tbody>
                      {users.map((user) => (
                        <tr key={user.id} className={colors.hover}>
                          <td className={`px-6 py-4 text-sm font-mono ${colors.textMuted}`}>{user.id}</td>
                          <td className="px-6 py-4">
                            <div className="text-sm font-medium">{user.name}</div>
                            <div className={`text-xs ${colors.textMuted}`}>{user.email}</div>
                          </td>
                          <td className="px-6 py-4">{user.plan}</td>
                          <td className="px-6 py-4">{user.tokens}</td>
                          <td className="px-6 py-4">
                            <span className={`inline-flex rounded-full px-2.5 py-1 text-xs font-medium border ${userStatusBadgeClass(user.statusCode)}`}>
                              {user.status}
                            </span>
                          </td>
                          <td className="px-6 py-4 text-right">
                            <div className="flex items-center justify-end gap-2">
                              <button
                                onClick={() => void handleUserStatusChange(user, user.statusCode === 'active' ? 'banned' : 'active')}
                                className={`px-3 py-1.5 rounded-md text-xs font-medium border ${
                                  user.statusCode === 'active'
                                    ? 'border-red-500/30 text-red-500 hover:bg-red-500/10'
                                    : 'border-green-500/30 text-green-500 hover:bg-green-500/10'
                                }`}
                              >
                                {user.statusCode === 'active' ? '封号' : '解封'}
                              </button>
                              <button onClick={() => openMemberConfig(user)} className={`px-3 py-1.5 rounded-md text-xs font-medium border ${colors.border} ${colors.hover}`}>
                                管理配置
                              </button>
                            </div>
                          </td>
                        </tr>
                      ))}
                    </tbody>
                  </table>
                </div>
              </div>
            </div>
          )}

          {currentView === 'models' && (
            <div className="space-y-6">
              <div className={`${panelClass} p-6 flex flex-col gap-4 md:flex-row md:items-center md:justify-between`}>
                <div>
                  <h2 className="text-2xl font-semibold">模型编排中心</h2>
                  <p className={`mt-2 text-sm ${colors.textMuted}`}>新增模型、切协议、改端点后，用户端聊天模型选择器会自动同步。</p>
                </div>
                <button onClick={() => openModelEditor()} className={`px-4 py-2.5 rounded-xl text-sm font-medium ${colors.btnPrimary}`}>
                  <PlusCircle className="w-4 h-4 inline mr-2" />
                  新增模型
                </button>
              </div>
              <div className={`${panelClass} p-6 flex flex-col gap-4 md:flex-row md:items-center md:justify-between`}>
                <div>
                  <h3 className="text-xl font-semibold">照片生成配置</h3>
                  <p className={`mt-2 text-sm ${colors.textMuted}`}>这块独立于聊天模型，默认走 `gpt-image-2`，供后端 `/v1/images/generations` 调用。</p>
                </div>
                <button onClick={openPhotoModelEditor} className={ghostButtonClass}>
                  {photoModel ? '编辑照片模型' : '初始化照片模型'}
                </button>
              </div>
              {photoModel && (
                <div className={`${panelClass} p-6 flex flex-col gap-5`}>
                  <div className="flex items-start justify-between gap-4">
                    <div className="space-y-2">
                      <div className="flex flex-wrap items-center gap-2">
                        <h3 className="text-lg font-semibold">{photoModel.name}</h3>
                        <span className={`px-2.5 py-1 rounded-full text-[11px] font-mono border ${colors.border} ${colors.textMuted}`}>{photoModel.slug}</span>
                      </div>
                      <p className={`text-sm ${colors.textMuted}`}>{photoModel.description || '暂无描述'}</p>
                    </div>
                    <div className={`px-2.5 py-1 rounded-full text-xs font-medium border ${photoModel.active ? 'border-green-500/30 text-green-500 bg-green-500/10' : 'border-yellow-500/30 text-yellow-500 bg-yellow-500/10'}`}>
                      {formatModelAvailability(photoModel.active)}
                    </div>
                  </div>
                  <div className="grid grid-cols-2 gap-3 text-sm">
                    <div className={`rounded-xl border p-3 ${colors.border}`}>
                      <div className={`${colors.textMuted} text-xs mb-1`}>模型类型</div>
                      <div className="font-medium">{formatModelType(photoModel.modelType)}</div>
                    </div>
                    <div className={`rounded-xl border p-3 ${colors.border}`}>
                      <div className={`${colors.textMuted} text-xs mb-1`}>协议 / 策略</div>
                      <div className="font-medium">{formatProtocol(photoModel.protocol)} / {formatStrategy(photoModel.strategy)}</div>
                    </div>
                    <div className={`rounded-xl border p-3 ${colors.border}`}>
                      <div className={`${colors.textMuted} text-xs mb-1`}>上游模型</div>
                      <div className="font-medium">{photoModel.upstreamModel || '-'}</div>
                    </div>
                    <div className={`rounded-xl border p-3 ${colors.border}`}>
                      <div className={`${colors.textMuted} text-xs mb-1`}>提示词注入</div>
                      <div className="font-medium">{photoModel.promptEnabled ? '已开启' : '未开启'}</div>
                    </div>
                  </div>
                  <ModelProbePanel probe={photoProbeResult} colors={colors} />
                  <div className={`pt-4 border-t flex flex-col gap-3 md:flex-row md:items-center md:justify-between ${colors.border}`}>
                    <span className={`text-xs ${colors.textMuted}`}>建议保存后点一次一键测通路，确认图片模型的上游地址和密钥真实可用。</span>
                    <div className="flex items-center gap-2">
                      <button
                        onClick={() => void handleProbeModel(photoModel)}
                        disabled={probingModelKey === modelProbeKey(photoModel)}
                        className={ghostButtonClass}
                      >
                        {probingModelKey === modelProbeKey(photoModel) ? '测试中...' : '一键测通路'}
                      </button>
                      <button onClick={openPhotoModelEditor} className={ghostButtonClass}>
                        编辑照片模型
                      </button>
                      <button onClick={() => void handleDeleteModel(photoModel)} className="px-4 py-2.5 rounded-xl text-sm font-medium border border-red-500/30 text-red-500 hover:bg-red-500/10">
                        删除模型
                      </button>
                    </div>
                  </div>
                </div>
              )}
              <div className={`${panelClass} p-6`}>
                <div className="flex items-center justify-between">
                  <div>
                    <h3 className="text-xl font-semibold">对话模型</h3>
                    <p className={`mt-2 text-sm ${colors.textMuted}`}>客户端聊天、深度搜索和开发者对话接口都从这里读取模型路由。</p>
                  </div>
                </div>
              </div>
              <div className="grid grid-cols-1 lg:grid-cols-2 gap-6">
                {chatModels.map((model) => (
                  <div key={model.id} className={`${panelClass} p-6 flex flex-col gap-5`}>
                    <div className="flex items-start justify-between gap-4">
                      <div className="space-y-2">
                        <div className="flex flex-wrap items-center gap-2">
                          <h3 className="text-lg font-semibold">{model.name}</h3>
                          <span className={`px-2.5 py-1 rounded-full text-[11px] font-mono border ${colors.border} ${colors.textMuted}`}>{model.slug}</span>
                        </div>
                        <p className={`text-sm ${colors.textMuted}`}>{model.description || '暂无描述'}</p>
                      </div>
                      <div className={`px-2.5 py-1 rounded-full text-xs font-medium border ${model.active ? 'border-green-500/30 text-green-500 bg-green-500/10' : 'border-yellow-500/30 text-yellow-500 bg-yellow-500/10'}`}>
                        {formatModelAvailability(model.active)}
                      </div>
                    </div>
                    <div className="grid grid-cols-2 gap-3 text-sm">
                      <div className={`rounded-xl border p-3 ${colors.border}`}>
                        <div className={`${colors.textMuted} text-xs mb-1`}>模型类型</div>
                        <div className="font-medium">{formatModelType(model.modelType)}</div>
                      </div>
                      <div className={`rounded-xl border p-3 ${colors.border}`}>
                        <div className={`${colors.textMuted} text-xs mb-1`}>协议 / 策略</div>
                        <div className="font-medium">{formatProtocol(model.protocol)} / {formatStrategy(model.strategy)}</div>
                      </div>
                      <div className={`rounded-xl border p-3 ${colors.border}`}>
                        <div className={`${colors.textMuted} text-xs mb-1`}>上游模型</div>
                        <div className="font-medium">{model.upstreamModel || '-'}</div>
                      </div>
                      <div className={`rounded-xl border p-3 ${colors.border}`}>
                        <div className={`${colors.textMuted} text-xs mb-1`}>提示词注入</div>
                        <div className="font-medium">{model.promptEnabled ? '已开启' : '未开启'}</div>
                      </div>
                      <div className={`rounded-xl border p-3 ${colors.border}`}>
                        <div className={`${colors.textMuted} text-xs mb-1`}>端点数量</div>
                        <div className="font-medium">{Array.isArray(model.endpoints) ? model.endpoints.length : 0}</div>
                      </div>
                    </div>
                    <ModelProbePanel probe={modelProbeResults[modelProbeKey(model)] ?? null} colors={colors} />
                    <div className={`pt-4 border-t flex flex-col gap-3 md:flex-row md:items-center md:justify-between ${colors.border}`}>
                      <span className={`text-xs ${colors.textMuted}`}>保存后会同步到用户端模型列表</span>
                      <div className="flex items-center gap-2">
                        <button
                          onClick={() => void handleProbeModel(model)}
                          disabled={probingModelKey === modelProbeKey(model)}
                          className={ghostButtonClass}
                        >
                          {probingModelKey === modelProbeKey(model) ? '测试中...' : '一键测通路'}
                        </button>
                        <button onClick={() => openModelEditor(model)} className={ghostButtonClass}>
                          配置模型
                        </button>
                        <button onClick={() => void handleDeleteModel(model)} className="px-4 py-2.5 rounded-xl text-sm font-medium border border-red-500/30 text-red-500 hover:bg-red-500/10">
                          删除模型
                        </button>
                      </div>
                    </div>
                  </div>
                ))}
              </div>
              {chatModels.length === 0 && (
                <div className={`${panelClass} p-10 text-center`}>
                  <div className="text-lg font-medium">还没有对话模型</div>
                  <p className={`mt-2 text-sm ${colors.textMuted}`}>先新增一个聊天模型，用户端和开发者 API 才能正常使用。</p>
                </div>
              )}
            </div>
          )}

          {currentView === 'after-sales' && (
            <div className="space-y-6">
              <div className={`${panelClass} p-6`}>
                <div className="flex flex-col gap-4 md:flex-row md:items-end md:justify-between">
                  <div>
                    <h2 className="text-2xl font-semibold">售后服务</h2>
                    <p className={`mt-2 text-sm ${colors.textMuted}`}>这里集中展示后台异常提醒。弹窗里点过“已读”的异常会继续留在这里，直到你点“已处理”为止。</p>
                  </div>
                  <div className="flex gap-3 text-sm">
                    <div className={`rounded-xl border px-4 py-3 ${colors.border}`}>
                      <div className={colors.textMuted}>待处理</div>
                      <div className="mt-1 text-xl font-semibold">{pendingServiceAlerts.length}</div>
                    </div>
                    <div className={`rounded-xl border px-4 py-3 ${colors.border}`}>
                      <div className={colors.textMuted}>未读</div>
                      <div className="mt-1 text-xl font-semibold">{unreadServiceAlerts.length}</div>
                    </div>
                  </div>
                </div>
              </div>
              <div className={`rounded-xl border overflow-hidden ${colors.cardBg} ${colors.border}`}>
                <table className="w-full text-left text-sm">
                  <thead>
                    <tr className={`border-b ${colors.border} ${colors.textMuted}`}>
                      <th className="px-6 py-4">账号</th>
                      <th className="px-6 py-4">报错时间</th>
                      <th className="px-6 py-4">来源</th>
                      <th className="px-6 py-4">报错内容</th>
                      <th className="px-6 py-4">状态</th>
                      <th className="px-6 py-4 text-right">操作</th>
                    </tr>
                  </thead>
                  <tbody>
                    {pendingServiceAlerts.map((alert) => (
                      <tr key={alert.id} className={colors.hover}>
                        <td className="px-6 py-4">
                          <div className="font-medium">{alert.account || '未知账号'}</div>
                          <div className={`mt-1 text-xs ${colors.textMuted}`}>{alert.model || alert.path || '-'}</div>
                        </td>
                        <td className="px-6 py-4">{alert.createdAt}</td>
                        <td className="px-6 py-4">
                          <div>{formatServiceAlertSource(alert.source)}</div>
                          <div className={`mt-1 text-xs ${colors.textMuted}`}>{alert.path || '-'}</div>
                        </td>
                        <td className="px-6 py-4 max-w-[520px]">
                          <div className="whitespace-pre-wrap break-words">{formatUserVisibleErrorDetail(alert.errorDetail, '未返回错误详情')}</div>
                        </td>
                        <td className="px-6 py-4">
                          <span className={`inline-flex rounded-full px-2.5 py-1 text-xs font-medium border ${serviceAlertBadgeClass(alert.status)}`}>
                            {formatServiceAlertStatus(alert.status)}
                          </span>
                        </td>
                        <td className="px-6 py-4">
                          <div className="flex items-center justify-end gap-2">
                            <button
                              onClick={() => void handleCopyServiceAlert(alert)}
                              className={`px-3 py-1.5 rounded-md text-xs font-medium border ${colors.border} ${colors.hover}`}
                            >
                              {copiedServiceAlertID === alert.id ? '已复制' : '复制完整报错'}
                            </button>
                            {alert.status === 'unread' && (
                              <button
                                onClick={() => void handleReadServiceAlert(alert.id)}
                                className={`px-3 py-1.5 rounded-md text-xs font-medium border ${colors.border} ${colors.hover}`}
                              >
                                已读
                              </button>
                            )}
                            <button
                              onClick={() => void handleResolveServiceAlert(alert.id)}
                              className={`px-3 py-1.5 rounded-md text-xs font-medium ${colors.btnPrimary}`}
                            >
                              已处理
                            </button>
                          </div>
                        </td>
                      </tr>
                    ))}
                    {pendingServiceAlerts.length === 0 && (
                      <tr>
                        <td className={`px-6 py-8 text-center ${colors.textMuted}`} colSpan={6}>当前没有待处理异常</td>
                      </tr>
                    )}
                  </tbody>
                </table>
              </div>
            </div>
          )}

          {currentView === 'api-stats' && (
            <div className="space-y-6">
              <div className="grid grid-cols-1 md:grid-cols-4 gap-6">
                <div className={`p-6 rounded-xl border ${colors.cardBg} ${colors.border}`}>
                  <div className={`text-sm font-medium mb-3 ${colors.textMuted}`}>最近请求数</div>
                  <div className="text-3xl font-semibold">{apiStats?.summary?.totalRequests ?? 0}</div>
                </div>
                <div className={`p-6 rounded-xl border ${colors.cardBg} ${colors.border}`}>
                  <div className={`text-sm font-medium mb-3 ${colors.textMuted}`}>成功率</div>
                  <div className="text-3xl font-semibold">{apiStats?.summary?.successRate ?? '0.0%'}</div>
                </div>
                <div className={`p-6 rounded-xl border ${colors.cardBg} ${colors.border}`}>
                  <div className={`text-sm font-medium mb-3 ${colors.textMuted}`}>平均延迟</div>
                  <div className="text-3xl font-semibold">{apiStats?.summary?.avgLatencyMs ?? 0}ms</div>
                </div>
                <div className={`p-6 rounded-xl border ${colors.cardBg} ${colors.border}`}>
                  <div className={`text-sm font-medium mb-3 ${colors.textMuted}`}>错误数</div>
                  <div className="text-3xl font-semibold">{apiStats?.summary?.errorCount ?? 0}</div>
                </div>
              </div>
              <div className={`rounded-xl border overflow-hidden ${colors.cardBg} ${colors.border}`}>
                <table className="w-full text-left text-sm">
                  <thead>
                    <tr className={`border-b ${colors.border} ${colors.textMuted}`}>
                      <th className="px-6 py-4">时间</th>
                      <th className="px-6 py-4">账号</th>
                      <th className="px-6 py-4">来源</th>
                      <th className="px-6 py-4">模型</th>
                      <th className="px-6 py-4">状态</th>
                      <th className="px-6 py-4">延迟</th>
                      <th className="px-6 py-4">错误详情</th>
                    </tr>
                  </thead>
                  <tbody>
                    {(apiStats?.logs ?? []).map((log: any, index: number) => (
                      <tr key={`${log.timestamp}-${index}`} className={colors.hover}>
                        <td className="px-6 py-4">{log.timestamp}</td>
                        <td className="px-6 py-4">{log.account || log.userId || '-'}</td>
                        <td className="px-6 py-4">{log.source}</td>
                        <td className="px-6 py-4">{log.model || '-'}</td>
                        <td className="px-6 py-4">{formatAPIStatus(log.status)}</td>
                        <td className="px-6 py-4">{log.latencyMs}ms</td>
                        <td className={`px-6 py-4 max-w-[360px] truncate ${colors.textMuted}`} title={formatUserVisibleErrorDetail(log.errorDetail)}>
                          {formatAPIErrorDetail(log.errorDetail)}
                        </td>
                      </tr>
                    ))}
                    {(apiStats?.logs ?? []).length === 0 && (
                      <tr>
                        <td className={`px-6 py-8 text-center ${colors.textMuted}`} colSpan={7}>暂无 API 调用记录</td>
                      </tr>
                    )}
                  </tbody>
                </table>
              </div>
            </div>
          )}

          {currentView === 'system-logs' && (
            <div className="space-y-6">
              <div className={`${panelClass} p-6 flex flex-col gap-4 md:flex-row md:items-end md:justify-between`}>
                <div>
                  <h2 className="text-2xl font-semibold">系统日志</h2>
                  <p className={`mt-2 text-sm ${colors.textMuted}`}>这里集中展示最近 7 天的系统日志。新日志会自动写入，7 天以前的日志会自动清理。验证码发送、登录、后台操作、接口访问都会落到这里。</p>
                </div>
                <div className="flex gap-3 text-sm">
                  <div className={`rounded-xl border px-4 py-3 ${colors.border}`}>
                    <div className={colors.textMuted}>最近 7 天</div>
                    <div className="mt-1 text-xl font-semibold">{systemLogs.length}</div>
                  </div>
                </div>
              </div>
              <div className={`${panelClass} p-6`}>
                <div className="flex flex-col gap-2 md:flex-row md:items-end md:justify-between">
                  <div>
                    <h3 className="text-lg font-semibold">最近验证码</h3>
                    <p className={`mt-2 text-sm ${colors.textMuted}`}>调试模式或真实网关产生的验证码会优先展示在这里，不用再去整张日志表里翻找。</p>
                  </div>
                  <div className={`text-sm ${colors.textMuted}`}>最近展示 {recentVerificationLogs.length} 条</div>
                </div>
                {recentVerificationLogs.length > 0 ? (
                  <div className="mt-5 grid gap-4 md:grid-cols-2 xl:grid-cols-4">
                    {recentVerificationLogs.map((log: any) => (
                      <div key={`verification-${log.id}`} className={`rounded-2xl border p-4 ${colors.border} ${colors.inputBg}`}>
                        <div className="flex items-start justify-between gap-3">
                          <div className="min-w-0">
                            <div className="truncate text-sm font-medium">{log.account || log.userId || log.adminId || '-'}</div>
                            <div className={`mt-1 text-xs ${colors.textMuted}`}>{formatSystemLogTime(log.createdAt)}</div>
                          </div>
                          <span className={`inline-flex rounded-full border px-2 py-1 text-[11px] ${colors.border} ${colors.textMuted}`}>
                            {getSystemLogDeliveryMode(log.payload)}
                          </span>
                        </div>
                        <div className="mt-4 inline-flex rounded-xl border border-blue-500/30 bg-blue-500/10 px-3 py-2 font-mono text-base font-semibold text-blue-500">
                          {getSystemLogCode(log.payload)}
                        </div>
                        <div className={`mt-3 text-xs ${colors.textMuted}`}>{log.message || '验证码已发送'}</div>
                      </div>
                    ))}
                  </div>
                ) : (
                  <div className={`mt-5 rounded-2xl border px-4 py-6 text-sm ${colors.border} ${colors.textMuted}`}>
                    最近 7 天还没有验证码发送记录。
                  </div>
                )}
              </div>
              <div className={`rounded-xl border overflow-hidden ${colors.cardBg} ${colors.border}`}>
                <table className="w-full text-left text-sm">
                  <thead>
                    <tr className={`border-b ${colors.border} ${colors.textMuted}`}>
                      <th className="px-6 py-4">时间</th>
                      <th className="px-6 py-4">服务</th>
                      <th className="px-6 py-4">类型</th>
                      <th className="px-6 py-4">账号</th>
                      <th className="px-6 py-4">路径</th>
                      <th className="px-6 py-4">状态</th>
                      <th className="px-6 py-4">验证码</th>
                      <th className="px-6 py-4">内容</th>
                      <th className="px-6 py-4">详情</th>
                    </tr>
                  </thead>
                  <tbody>
                    {systemLogs.map((log: any) => (
                      <tr key={log.id} className={colors.hover}>
                        <td className="px-6 py-4 whitespace-nowrap">{formatSystemLogTime(log.createdAt)}</td>
                        <td className="px-6 py-4">{formatSystemLogService(log.service)}</td>
                        <td className="px-6 py-4">
                          <div>{formatSystemLogCategory(log.category)}</div>
                          <div className={`mt-1 text-xs ${colors.textMuted}`}>{log.eventType || '-'}</div>
                        </td>
                        <td className="px-6 py-4">
                          <div>{log.account || log.userId || log.adminId || '-'}</div>
                          <div className={`mt-1 text-xs ${colors.textMuted}`}>{log.ip || '-'}</div>
                        </td>
                        <td className="px-6 py-4">
                          <div>{log.path || '-'}</div>
                          <div className={`mt-1 text-xs ${colors.textMuted}`}>{log.method || '-'}</div>
                        </td>
                        <td className="px-6 py-4">
                          <span className={`inline-flex rounded-full border px-2.5 py-1 text-xs font-medium ${systemLogBadgeClass(log.level, log.statusCode)}`}>
                            {formatSystemLogStatus(log.statusCode, log.level)}
                          </span>
                        </td>
                        <td className="px-6 py-4">
                          {getSystemLogCode(log.payload) ? (
                            <div>
                              <div className="inline-flex rounded-lg border border-blue-500/30 bg-blue-500/10 px-2.5 py-1 font-mono text-sm text-blue-500">
                                {getSystemLogCode(log.payload)}
                              </div>
                              <div className={`mt-1 text-xs ${colors.textMuted}`}>{getSystemLogDeliveryMode(log.payload)}</div>
                            </div>
                          ) : (
                            <span className={colors.textMuted}>-</span>
                          )}
                        </td>
                        <td className="px-6 py-4 max-w-[280px]">
                          <div className="whitespace-pre-wrap break-words">{log.message || '-'}</div>
                        </td>
                        <td className={`px-6 py-4 max-w-[360px] whitespace-pre-wrap break-words text-xs leading-6 ${colors.textMuted}`}>
                          {formatSystemLogPayload(log.payload)}
                        </td>
                      </tr>
                    ))}
                    {systemLogs.length === 0 && (
                      <tr>
                        <td className={`px-6 py-8 text-center ${colors.textMuted}`} colSpan={9}>最近 7 天暂无系统日志</td>
                      </tr>
                    )}
                  </tbody>
                </table>
              </div>
            </div>
          )}

          {currentView === 'member-stats' && (
            <div className={`rounded-xl border overflow-hidden ${colors.cardBg} ${colors.border}`}>
              <table className="w-full text-left">
                <thead>
                  <tr className={`border-b ${colors.border}`}>
                    <th className="px-6 py-4">用户</th>
                    <th className="px-6 py-4">动作</th>
                    <th className="px-6 py-4">金额</th>
                    <th className="px-6 py-4">时间</th>
                  </tr>
                </thead>
                <tbody>
                  {memberLogs.map((log, index) => (
                    <tr key={`${log.user}-${index}`} className={colors.hover}>
                      <td className="px-6 py-4">{log.user}</td>
                      <td className="px-6 py-4">{log.action}</td>
                      <td className="px-6 py-4">{log.amount}</td>
                      <td className="px-6 py-4">{log.date}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}

          {currentView === 'membership' && (
            <div className="space-y-6">
              <div className="flex justify-between items-center">
                <div>
                  <h2 className="text-2xl font-medium">会员订阅管理</h2>
                  <p className={`text-sm mt-1 ${colors.textMuted}`}>查看并管理已订阅付费套餐的用户及其详细配置。</p>
                </div>
                <button onClick={() => setShowGiftModal(true)} className={`px-4 py-2 text-sm font-medium rounded-md ${colors.btnPrimary}`}>
                  <Gift className="w-4 h-4 inline mr-2" /> 礼品卡兑换生成
                </button>
              </div>
              <div className={`rounded-xl border overflow-hidden ${colors.cardBg} ${colors.border}`}>
                <table className="w-full text-left">
                  <thead>
                    <tr className={`border-b ${colors.border}`}>
                      <th className="px-6 py-4">用户</th>
                      <th className="px-6 py-4">套餐</th>
                      <th className="px-6 py-4">状态</th>
                      <th className="px-6 py-4 text-right">操作</th>
                    </tr>
                  </thead>
                  <tbody>
                    {users.map((user) => (
                      <tr key={user.id} className={colors.hover}>
                        <td className="px-6 py-4">
                          <div className="font-medium">{user.name}</div>
                          <div className={`text-xs mt-1 ${colors.textMuted}`}>{user.email}</div>
                        </td>
                        <td className="px-6 py-4">{user.plan}</td>
                        <td className="px-6 py-4">
                          <span className={`inline-flex rounded-full px-2.5 py-1 text-xs font-medium border ${userStatusBadgeClass(user.statusCode)}`}>
                            {user.status}
                          </span>
                        </td>
                        <td className="px-6 py-4">
                          <div className="flex items-center justify-end gap-2">
                            <button
                              onClick={() => void handleQuickMembershipChange(user, 'downgrade')}
                              disabled={!getAdjacentPlan(user.rawPlan, 'downgrade')}
                              className={`px-3 py-1.5 rounded-md text-xs font-medium border ${colors.border} ${colors.hover} disabled:cursor-not-allowed disabled:opacity-40`}
                            >
                              降级
                            </button>
                            <button
                              onClick={() => void handleQuickMembershipChange(user, 'upgrade')}
                              disabled={!getAdjacentPlan(user.rawPlan, 'upgrade')}
                              className={`px-3 py-1.5 rounded-md text-xs font-medium border ${colors.border} ${colors.hover} disabled:cursor-not-allowed disabled:opacity-40`}
                            >
                              升级
                            </button>
                            <button
                              onClick={() => void handleUserStatusChange(user, user.statusCode === 'active' ? 'banned' : 'active')}
                              className={`px-3 py-1.5 rounded-md text-xs font-medium border ${
                                user.statusCode === 'active'
                                  ? 'border-red-500/30 text-red-500 hover:bg-red-500/10'
                                  : 'border-green-500/30 text-green-500 hover:bg-green-500/10'
                              }`}
                            >
                              {user.statusCode === 'active' ? '封号' : '解封'}
                            </button>
                            <button
                              onClick={() => openMemberConfig(user)}
                              className={`px-3 py-1.5 rounded-md text-xs font-medium border ${colors.border} ${colors.hover}`}
                            >
                              详细配置
                            </button>
                          </div>
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
              <div className={`${panelClass} p-6 space-y-5`}>
                <div className="flex flex-col gap-4 md:flex-row md:items-center md:justify-between">
	                  <div>
	                    <h3 className="text-xl font-semibold">模型套餐权限、配额与上下文长度</h3>
	                    <p className={`mt-2 text-sm ${colors.textMuted}`}>后台可以控制每个模型允许哪些套餐使用、24 小时成功回复次数上限，以及不同套餐的上下文输入长度。上下文限制只裁剪输入历史，不截断正在输出的文章。</p>
	                  </div>
	                  <div className="flex flex-wrap gap-2">
	                    <button onClick={() => void saveModelMembershipLimits()} className={`px-4 py-2.5 rounded-xl text-sm font-medium ${colors.btnPrimary}`}>
	                      保存模型权限
	                    </button>
	                    <button onClick={() => void saveModelContextLimits()} className={`px-4 py-2.5 rounded-xl text-sm font-medium border ${colors.border} ${colors.hover}`}>
	                      保存上下文长度
	                    </button>
	                  </div>
                </div>
                <div className={`rounded-2xl border p-4 ${colors.border}`}>
                  <div className="text-sm font-medium">全局默认上下文长度</div>
                  <p className={`mt-1 text-xs ${colors.textMuted}`}>优先级为：账号覆盖、套餐模型配置、模型默认、全局默认；留空或 0 表示不限制输入历史。</p>
                  <input
                    value={settings?.modelContextLimits?.default ?? 0}
                    onChange={(event) => setSettings((prev: any) => ({ ...prev, modelContextLimits: { ...(prev?.modelContextLimits ?? {}), default: Number(event.target.value) || 0 } }))}
                    className={`${inputClass} mt-3 max-w-xs`}
                    type="number"
                    min={0}
                    step={256}
                    placeholder="全局默认上下文长度"
                  />
                </div>
                <div className="space-y-4">
                  {chatModels.map((model) => (
                    <div key={model.slug} className={`rounded-2xl border p-5 ${colors.border}`}>
                      <div className="flex flex-col gap-2 md:flex-row md:items-center md:justify-between">
                        <div>
                          <div className="text-base font-semibold">{model.name || model.slug}</div>
                          <div className={`mt-1 text-sm ${colors.textMuted}`}>{model.slug}</div>
                        </div>
                        <div className="flex flex-col gap-2 md:min-w-[240px]">
                          <div className={`text-xs ${colors.textMuted}`}>只统计成功生成的助手回复</div>
                          <input
                            value={getModelDefaultContextLimit(model.slug) === undefined || getModelDefaultContextLimit(model.slug) === null ? '' : String(getModelDefaultContextLimit(model.slug))}
                            onChange={(event) => setModelDefaultContextLimit(model.slug, event.target.value)}
                            className={inputClass}
                            type="number"
                            min={256}
                            step={256}
                            placeholder="模型默认上下文长度"
                          />
                        </div>
                      </div>
                      <div className="mt-4 grid grid-cols-1 gap-3 md:grid-cols-2 xl:grid-cols-5">
	                        {MEMBERSHIP_PLAN_OPTIONS.map((plan) => {
	                          const rawLimit = getModelPlanLimit(plan.code, model.slug)
	                          const isAllowed = rawLimit !== 0 && rawLimit !== '0'
	                          const quotaValue = isAllowed && rawLimit !== undefined && rawLimit !== null ? String(rawLimit) : ''
	                          const contextLimitValue = getModelPlanContextLimit(plan.code, model.slug)
	                          return (
	                            <div key={`${model.slug}-${plan.code}`} className={`rounded-xl border p-3 ${colors.border}`}>
                              <div className="flex items-center justify-between gap-3">
                                <div className={`text-xs font-medium ${colors.textMuted}`}>{plan.label}</div>
                                <label className="flex items-center gap-2 text-xs font-medium">
                                  <input
                                    type="checkbox"
                                    checked={isAllowed}
                                    onChange={(event) => setModelPlanAvailability(plan.code, model.slug, event.target.checked)}
                                  />
                                  <span>{isAllowed ? '允许使用' : '禁止使用'}</span>
                                </label>
                              </div>
                              <input
                                value={quotaValue}
                                onChange={(event) => setModelPlanQuota(plan.code, model.slug, event.target.value)}
                                className={`${inputClass} mt-3 ${!isAllowed ? 'opacity-50 cursor-not-allowed' : ''}`}
                                type="number"
                                min={1}
                                step={1}
                                placeholder={isAllowed ? '留空=不限次数' : '当前套餐不可用'}
                                disabled={!isAllowed}
                              />
	                              <div className={`mt-2 text-[11px] ${colors.textMuted}`}>
	                                {isAllowed ? '24 小时成功回复次数上限' : '保存后该套餐将无法选择这个模型'}
	                              </div>
	                              <input
	                                value={contextLimitValue === undefined || contextLimitValue === null ? '' : String(contextLimitValue)}
	                                onChange={(event) => setModelPlanContextLimit(plan.code, model.slug, event.target.value)}
	                                className={`${inputClass} mt-3 ${!isAllowed ? 'opacity-50 cursor-not-allowed' : ''}`}
	                                type="number"
	                                min={256}
	                                step={256}
	                                placeholder={isAllowed ? '上下文长度，留空=不限' : '套餐不可用'}
	                                disabled={!isAllowed}
	                              />
	                              <div className={`mt-2 text-[11px] ${colors.textMuted}`}>上下文输入长度</div>
	                            </div>
	                          )
                        })}
                      </div>
                    </div>
                  ))}
                  {chatModels.length === 0 && (
                    <div className={`rounded-2xl border border-dashed p-6 text-sm ${colors.border} ${colors.textMuted}`}>
                      先在模型管理里创建对话模型，这里才会出现可配置的会员配额。
                    </div>
                  )}
                </div>
              </div>
              <div className={`${panelClass} p-6 space-y-5`}>
                <div className="flex flex-col gap-4 md:flex-row md:items-center md:justify-between">
                  <div>
                    <h3 className="text-xl font-semibold">Infinite Code 周期额度</h3>
                    <p className={`mt-2 text-sm ${colors.textMuted}`}>为每个会员档位配置 Infinite Code 编程助手的单周期可用次数，以及每多少小时自动恢复一次。</p>
                  </div>
                  <button onClick={() => void saveInfiniteCodeQuotaConfig()} className={`px-4 py-2.5 rounded-xl text-sm font-medium ${colors.btnPrimary}`}>
                    保存 Infinite Code 配额
                  </button>
                </div>
                <div className="grid grid-cols-1 gap-4 md:grid-cols-2 xl:grid-cols-5">
                  {MEMBERSHIP_PLAN_OPTIONS.map((plan) => (
                    <div key={`infinite-code-${plan.code}`} className={`rounded-2xl border p-5 ${colors.border}`}>
                      <div className="text-base font-semibold">{plan.label}</div>
                      <div className={`mt-1 text-xs ${colors.textMuted}`}>当前套餐的周期额度设置</div>
                      <div className="mt-4 space-y-3">
                        <div>
                          <div className={`mb-2 text-xs font-medium ${colors.textMuted}`}>每次恢复额度</div>
                          <input
                            value={settings?.infiniteCodeQuotaConfig?.[plan.code]?.credits ?? ''}
                            onChange={(event) =>
                              setSettings((prev: any) => ({
                                ...(prev ?? {}),
                                infiniteCodeQuotaConfig: {
                                  ...(prev?.infiniteCodeQuotaConfig ?? {}),
                                  [plan.code]: {
                                    ...(prev?.infiniteCodeQuotaConfig?.[plan.code] ?? {}),
                                    credits: Number(event.target.value) || 0,
                                  },
                                },
                              }))
                            }
                            className={inputClass}
                            type="number"
                            min={0}
                            step={1}
                          />
                        </div>
                        <div>
                          <div className={`mb-2 text-xs font-medium ${colors.textMuted}`}>恢复周期（小时）</div>
                          <input
                            value={settings?.infiniteCodeQuotaConfig?.[plan.code]?.resetHours ?? ''}
                            onChange={(event) =>
                              setSettings((prev: any) => ({
                                ...(prev ?? {}),
                                infiniteCodeQuotaConfig: {
                                  ...(prev?.infiniteCodeQuotaConfig ?? {}),
                                  [plan.code]: {
                                    ...(prev?.infiniteCodeQuotaConfig?.[plan.code] ?? {}),
                                    resetHours: Number(event.target.value) || 24,
                                  },
                                },
                              }))
                            }
                            className={inputClass}
                            type="number"
                            min={1}
                            step={1}
                          />
                        </div>
                      </div>
                    </div>
                  ))}
                </div>
              </div>
            </div>
          )}

          {currentView === 'finance-management' && (
            <div className="space-y-6">
              <div className="grid grid-cols-1 md:grid-cols-3 gap-6">
                <div className={`${panelClass} p-6`}>
                  <div className={`text-sm font-medium mb-3 ${colors.textMuted}`}>今日总营收</div>
                  <div className="text-3xl font-semibold mb-2">¥{finance?.todayRevenue ?? 0}</div>
                </div>
                <div className={`${panelClass} p-6`}>
                  <div className={`text-sm font-medium mb-3 ${colors.textMuted}`}>本月累计营收</div>
                  <div className="text-3xl font-semibold mb-2">¥{finance?.monthRevenue ?? 0}</div>
                </div>
                <div className={`${panelClass} p-6`}>
                  <div className={`text-sm font-medium mb-3 ${colors.textMuted}`}>待处理金额</div>
                  <div className="text-3xl font-semibold mb-2">¥{finance?.pendingAmount ?? 0}</div>
                </div>
              </div>
              <div className={`${sectionClass} overflow-hidden`}>
                <div className={`p-6 border-b ${colors.border} flex items-center gap-3`}>
                  <Wallet className="w-5 h-5 text-blue-500" />
                  <h3 className="text-lg font-medium">最近财务流水</h3>
                </div>
                <div className="overflow-x-auto">
                  <table className="w-full text-left min-w-[820px]">
                    <thead>
                      <tr className={`border-b text-xs uppercase tracking-wider ${colors.border} ${colors.textMuted}`}>
                        <th className="px-6 py-4 font-medium">订单号</th>
                        <th className="px-6 py-4 font-medium">用户</th>
                        <th className="px-6 py-4 font-medium">类型</th>
                        <th className="px-6 py-4 font-medium">金额</th>
                        <th className="px-6 py-4 font-medium">状态</th>
                        <th className="px-6 py-4 font-medium">时间</th>
                      </tr>
                    </thead>
                    <tbody>
                      {(finance?.transactions ?? []).map((item: any, index: number) => (
                        <tr key={`${item.id || item.orderId || index}`} className={colors.hover}>
                          <td className="px-6 py-4 text-sm font-mono">{item.orderId || item.id || '-'}</td>
                          <td className="px-6 py-4">
                            <div>{item.account || item.userName || '-'}</div>
                            <div className={`mt-1 text-xs ${colors.textMuted}`}>{item.email || '-'}</div>
                          </td>
                          <td className="px-6 py-4">{item.type || '-'}</td>
                          <td className="px-6 py-4">¥{item.amount ?? 0}</td>
                          <td className="px-6 py-4">{item.status || '-'}</td>
                          <td className="px-6 py-4">{item.createdAt || '-'}</td>
                        </tr>
                      ))}
                      {(finance?.transactions ?? []).length === 0 && (
                        <tr>
                          <td className={`px-6 py-8 text-center ${colors.textMuted}`} colSpan={6}>暂时还没有财务流水</td>
                        </tr>
                      )}
                    </tbody>
                  </table>
                </div>
              </div>
            </div>
          )}

          {currentView === 'finance' && (
            <div className="space-y-6">
              <div className={`${sectionClass} overflow-hidden`}>
                <div className={`p-6 border-b ${colors.border} flex items-center gap-3`}>
                  <Wallet className="w-5 h-5 text-blue-500" />
                  <h3 className="text-lg font-medium">IF-Pay 网关配置</h3>
                </div>
                <div className="p-6 space-y-4">
                  <input value={finance?.ifpayConfig?.merchantId ?? ''} onChange={(event) => setFinance((prev: any) => ({ ...prev, ifpayConfig: { ...prev.ifpayConfig, merchantId: event.target.value } }))} className={inputClass} placeholder="Merchant ID" />
                  <input value={finance?.ifpayConfig?.secretKey ?? ''} onChange={(event) => setFinance((prev: any) => ({ ...prev, ifpayConfig: { ...prev.ifpayConfig, secretKey: event.target.value } }))} className={inputClass} placeholder="Secret Key" />
                  <input value={finance?.ifpayConfig?.webhookURL ?? ''} onChange={(event) => setFinance((prev: any) => ({ ...prev, ifpayConfig: { ...prev.ifpayConfig, webhookURL: event.target.value } }))} className={inputClass} placeholder="Webhook URL" />
                  <div className={`rounded-xl border px-4 py-3 text-sm ${colors.border}`}>
                    <div className={`mb-1 ${colors.textMuted}`}>建议回调地址</div>
                    <div className="break-all font-mono">{finance?.webhookHintURL || '-'}</div>
                  </div>
                  <button onClick={async () => { await api.adminUpdateIFPay(finance.ifpayConfig); setMessage('IF-Pay 配置已保存'); await refresh() }} className={`px-6 py-2.5 rounded-xl text-sm font-medium ${colors.btnPrimary}`}>保存网关配置</button>
                </div>
              </div>
            </div>
          )}

          {currentView === 'settings' && (
            <div className="space-y-6 max-w-5xl">
              <div className={`${sectionClass} overflow-hidden`}>
                <div className={`p-6 border-b ${colors.border}`}>
                  <h3 className="text-base font-medium mb-1">控制台主题</h3>
                  <select value={theme} onChange={(event) => setTheme(event.target.value as 'dark' | 'light')} className={`${inputClass} max-w-xs`}>
                    <option value="dark">深色主题</option>
                    <option value="light">浅色主题</option>
                  </select>
                </div>
                <div className={`p-6 border-b ${colors.border}`}>
                  <div className="flex flex-col gap-4 md:flex-row md:items-start md:justify-between">
                    <div>
                      <h3 className="text-base font-medium mb-1">系统注册开关</h3>
                      <p className={`text-sm ${colors.textMuted}`}>关闭后，邮箱密码注册和首次 OAuth 自动建号都会被拦截。</p>
                    </div>
                    <label className="flex items-center gap-3 cursor-pointer">
                      <input type="checkbox" checked={Boolean(settings?.registerEnabled)} onChange={(event) => setSettings((prev: any) => ({ ...prev, registerEnabled: event.target.checked }))} />
                      <span className="text-sm font-medium">允许新注册</span>
                    </label>
                  </div>
                  <button
                    onClick={async () => {
                      await api.adminUpdateRegister(Boolean(settings?.registerEnabled))
                      setNotice({
                        title: settings?.registerEnabled ? '注册已开启' : '注册已关闭',
                        body: settings?.registerEnabled ? '新用户现在可以通过邮箱注册和合规 OAuth 建号。' : '新用户注册入口已被关闭，现有账号不受影响。',
                      })
                      await refresh()
                    }}
                    className={`mt-4 px-4 py-2.5 rounded-xl text-sm font-medium ${colors.btnPrimary}`}
                  >
                    保存注册开关
                  </button>
                </div>
                <div className={`p-6 border-b ${colors.border}`}>
                  <h3 className="text-base font-medium mb-2">注册安全策略</h3>
                  <p className={`text-sm mb-4 ${colors.textMuted}`}>控制图形验证码、短信验证、手机号登录，以及验证码是否切到后台系统日志调试模式。</p>
                  <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
                    <label className={`flex items-center justify-between rounded-xl border px-4 py-3 ${colors.border}`}>
                      <span className="text-sm font-medium">注册启用图形验证码</span>
                      <input type="checkbox" checked={Boolean(settings?.authSecurity?.captchaRequiredOnRegister)} onChange={(event) => setSettings((prev: any) => ({ ...prev, authSecurity: { ...prev.authSecurity, captchaRequiredOnRegister: event.target.checked } }))} />
                    </label>
                    <label className={`flex items-center justify-between rounded-xl border px-4 py-3 ${colors.border}`}>
                      <span className="text-sm font-medium">注册强制短信验证</span>
                      <input type="checkbox" checked={Boolean(settings?.authSecurity?.phoneVerificationRequiredOnRegister)} onChange={(event) => setSettings((prev: any) => ({ ...prev, authSecurity: { ...prev.authSecurity, phoneVerificationRequiredOnRegister: event.target.checked } }))} />
                    </label>
                    <label className={`flex items-center justify-between rounded-xl border px-4 py-3 ${colors.border}`}>
                      <span className="text-sm font-medium">允许手机号 + 密码登录</span>
                      <input type="checkbox" checked={Boolean(settings?.authSecurity?.phoneLoginEnabled)} onChange={(event) => setSettings((prev: any) => ({ ...prev, authSecurity: { ...prev.authSecurity, phoneLoginEnabled: event.target.checked } }))} />
                    </label>
                    <div className={`rounded-xl border px-4 py-3 ${colors.border}`}>
                      <div className="text-sm font-medium mb-2">短信验证码有效期</div>
                      <input value={settings?.authSecurity?.smsCodeTTLSeconds ?? 300} onChange={(event) => setSettings((prev: any) => ({ ...prev, authSecurity: { ...prev.authSecurity, smsCodeTTLSeconds: Number(event.target.value) || 300 } }))} className={inputClass} type="number" min={60} step={30} />
                    </div>
                  </div>
                  <div className={`mt-4 rounded-2xl border px-4 py-4 ${colors.border}`}>
                    <div className="flex flex-col gap-4 md:flex-row md:items-center md:justify-between">
                      <div>
                        <div className="text-sm font-medium">验证码调试模式</div>
                        <div className={`mt-1 text-xs leading-6 ${colors.textMuted}`}>
                          {settings?.authSecurity?.verificationTestMode
                            ? '当前已开启。邮箱和手机验证码都会直接写入后台系统日志，用户端不会因为网关未配置而报错。'
                            : '当前已关闭。邮箱和手机验证码会优先走你在下面配置的真实邮箱网关和短信网关。'}
                        </div>
                      </div>
                      <button
                        onClick={async () => {
                          const nextEnabled = !settings?.authSecurity?.verificationTestMode
                          const nextAuthSecurity = { ...(settings?.authSecurity ?? {}), verificationTestMode: nextEnabled }
                          try {
                            await api.adminUpdateAuthSecurity(nextAuthSecurity)
                            setSettings((prev: any) => ({ ...prev, authSecurity: nextAuthSecurity }))
                            setNotice({
                              title: nextEnabled ? '调试模式已开启' : '调试模式已关闭',
                              body: nextEnabled
                                ? '现在邮箱和手机验证码都会写入系统日志，用户端不会再因为网关未配置而报错。'
                                : '现在邮箱和手机验证码将恢复走你配置的真实邮箱网关和短信网关。',
                            })
                            await refresh()
                          } catch (error) {
                            setNotice({
                              title: nextEnabled ? '开启调试模式失败' : '关闭调试模式失败',
                              body: error instanceof Error ? error.message : '验证码调试模式切换失败，请稍后再试。',
                            })
                          }
                        }}
                        className={`px-4 py-2.5 rounded-xl text-sm font-medium ${settings?.authSecurity?.verificationTestMode ? 'border border-amber-500/30 text-amber-500 hover:bg-amber-500/10' : colors.btnPrimary}`}
                      >
                        {settings?.authSecurity?.verificationTestMode ? '关闭调试模式' : '开启调试模式'}
                      </button>
                    </div>
                  </div>
                  <button onClick={async () => { await api.adminUpdateAuthSecurity(settings?.authSecurity); setMessage('注册安全策略已保存'); await refresh() }} className={`mt-4 px-4 py-2.5 rounded-xl text-sm font-medium ${colors.btnPrimary}`}>保存安全策略</button>
                </div>
                <div className={`p-6 border-b ${colors.border}`}>
                  <h3 className="text-base font-medium mb-2">邮箱网关配置</h3>
                  <p className={`text-sm mb-4 ${colors.textMuted}`}>这里接上真实邮件服务后，邮箱注册会直接发送验证码。调试模式开启时，用户端邮箱验证码不会真正发信，而是只写系统日志。</p>
                  <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
                    <label className={`flex items-center justify-between rounded-xl border px-4 py-3 ${colors.border}`}>
                      <span className="text-sm font-medium">启用邮箱网关</span>
                      <input type="checkbox" checked={Boolean(settings?.emailGateway?.enabled)} onChange={(event) => setSettings((prev: any) => ({ ...prev, emailGateway: { ...prev.emailGateway, enabled: event.target.checked } }))} />
                    </label>
                    <input value={settings?.emailGateway?.providerName ?? ''} onChange={(event) => setSettings((prev: any) => ({ ...prev, emailGateway: { ...prev.emailGateway, providerName: event.target.value } }))} className={inputClass} placeholder="服务商名称" />
                    <input value={settings?.emailGateway?.endpointUrl ?? ''} onChange={(event) => setSettings((prev: any) => ({ ...prev, emailGateway: { ...prev.emailGateway, endpointUrl: event.target.value } }))} className={`${inputClass} md:col-span-2`} placeholder="邮件发送接口 URL" />
                    <select value={settings?.emailGateway?.authScheme ?? 'bearer'} onChange={(event) => setSettings((prev: any) => ({ ...prev, emailGateway: { ...prev.emailGateway, authScheme: event.target.value } }))} className={inputClass}>
                      <option value="bearer">Bearer 令牌</option>
                      <option value="header">自定义请求头</option>
                      <option value="none">不附加鉴权头</option>
                    </select>
                    <input value={settings?.emailGateway?.headerName ?? ''} onChange={(event) => setSettings((prev: any) => ({ ...prev, emailGateway: { ...prev.emailGateway, headerName: event.target.value } }))} className={inputClass} placeholder="请求头名称" />
                    <input value={settings?.emailGateway?.authToken ?? ''} onChange={(event) => setSettings((prev: any) => ({ ...prev, emailGateway: { ...prev.emailGateway, authToken: event.target.value } }))} className={`${inputClass} md:col-span-2`} placeholder="鉴权令牌 / API Key" />
                    <input value={settings?.emailGateway?.fromAddress ?? ''} onChange={(event) => setSettings((prev: any) => ({ ...prev, emailGateway: { ...prev.emailGateway, fromAddress: event.target.value } }))} className={inputClass} placeholder="发件邮箱" />
                    <input value={settings?.emailGateway?.fromName ?? ''} onChange={(event) => setSettings((prev: any) => ({ ...prev, emailGateway: { ...prev.emailGateway, fromName: event.target.value } }))} className={inputClass} placeholder="发件人名称" />
                    <input value={settings?.emailGateway?.subjectTemplate ?? ''} onChange={(event) => setSettings((prev: any) => ({ ...prev, emailGateway: { ...prev.emailGateway, subjectTemplate: event.target.value } }))} className={`${inputClass} md:col-span-2`} placeholder="邮件标题模板，支持 {{code}} 和 {{minutes}}" />
                    <textarea value={settings?.emailGateway?.contentTemplate ?? ''} onChange={(event) => setSettings((prev: any) => ({ ...prev, emailGateway: { ...prev.emailGateway, contentTemplate: event.target.value } }))} className={`${inputClass} md:col-span-2 min-h-28`} placeholder="邮件内容模板，支持 {{code}} 和 {{minutes}}" />
                  </div>
                  <button onClick={async () => { await api.adminUpdateEmailGateway(settings?.emailGateway); setMessage('邮箱网关配置已保存'); await refresh() }} className={`mt-4 px-4 py-2.5 rounded-xl text-sm font-medium ${colors.btnPrimary}`}>保存邮箱网关</button>
                </div>
                <div className={`p-6 border-b ${colors.border}`}>
                  <h3 className="text-base font-medium mb-2">短信网关配置</h3>
                  <p className={`text-sm mb-4 ${colors.textMuted}`}>这里接上真实短信服务后，注册页会开始发手机验证码。调试模式开启时，用户端手机验证码不会真正发短信，而是只写系统日志。</p>
                  <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
                    <label className={`flex items-center justify-between rounded-xl border px-4 py-3 ${colors.border}`}>
                      <span className="text-sm font-medium">启用短信网关</span>
                      <input type="checkbox" checked={Boolean(settings?.smsGateway?.enabled)} onChange={(event) => setSettings((prev: any) => ({ ...prev, smsGateway: { ...prev.smsGateway, enabled: event.target.checked } }))} />
                    </label>
                    <input value={settings?.smsGateway?.providerName ?? ''} onChange={(event) => setSettings((prev: any) => ({ ...prev, smsGateway: { ...prev.smsGateway, providerName: event.target.value } }))} className={inputClass} placeholder="服务商名称" />
                    <input value={settings?.smsGateway?.endpointUrl ?? ''} onChange={(event) => setSettings((prev: any) => ({ ...prev, smsGateway: { ...prev.smsGateway, endpointUrl: event.target.value } }))} className={`${inputClass} md:col-span-2`} placeholder="短信发送接口 URL" />
                    <select value={settings?.smsGateway?.authScheme ?? 'bearer'} onChange={(event) => setSettings((prev: any) => ({ ...prev, smsGateway: { ...prev.smsGateway, authScheme: event.target.value } }))} className={inputClass}>
                      <option value="bearer">Bearer 令牌</option>
                      <option value="header">自定义请求头</option>
                      <option value="none">不附加鉴权头</option>
                    </select>
                    <input value={settings?.smsGateway?.headerName ?? ''} onChange={(event) => setSettings((prev: any) => ({ ...prev, smsGateway: { ...prev.smsGateway, headerName: event.target.value } }))} className={inputClass} placeholder="请求头名称" />
                    <input value={settings?.smsGateway?.authToken ?? ''} onChange={(event) => setSettings((prev: any) => ({ ...prev, smsGateway: { ...prev.smsGateway, authToken: event.target.value } }))} className={`${inputClass} md:col-span-2`} placeholder="鉴权令牌 / API Key" />
                    <input value={settings?.smsGateway?.senderId ?? ''} onChange={(event) => setSettings((prev: any) => ({ ...prev, smsGateway: { ...prev.smsGateway, senderId: event.target.value } }))} className={inputClass} placeholder="发送方 ID" />
                    <textarea value={settings?.smsGateway?.messageTemplate ?? ''} onChange={(event) => setSettings((prev: any) => ({ ...prev, smsGateway: { ...prev.smsGateway, messageTemplate: event.target.value } }))} className={`${inputClass} md:col-span-2 min-h-28`} placeholder="短信模板，支持 {{code}} 和 {{minutes}}" />
                  </div>
                  <button onClick={async () => { await api.adminUpdateSMSGateway(settings?.smsGateway); setMessage('短信网关配置已保存'); await refresh() }} className={`mt-4 px-4 py-2.5 rounded-xl text-sm font-medium ${colors.btnPrimary}`}>保存短信网关</button>
                </div>
                <div className={`p-6 border-b ${colors.border}`}>
                  <h3 className="text-base font-medium mb-2">联网检索配置</h3>
                  <p className={`text-sm mb-4 ${colors.textMuted}`}>深度搜索会优先调用 OpenAI/Claude 以及兼容端点的原生 Web Search；原生不可用时自动回退到本地 SearXNG，并继续生成来源卡片和引用注入。</p>
                  <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
                    <label className={`flex items-center justify-between rounded-xl border px-4 py-3 ${colors.border}`}>
                      <span className="text-sm font-medium">启用联网检索</span>
                      <input type="checkbox" checked={settings?.searchProvider?.enabled !== false} onChange={(event) => setSettings((prev: any) => ({ ...prev, searchProvider: { ...prev.searchProvider, enabled: event.target.checked } }))} />
                    </label>
                    <select value={settings?.searchProvider?.provider ?? 'openai_then_searxng'} onChange={(event) => setSettings((prev: any) => ({ ...prev, searchProvider: { ...prev.searchProvider, provider: event.target.value } }))} className={inputClass}>
                      <option value="openai_then_searxng">官方原生优先，SearXNG 兜底</option>
                    </select>
                    <input value={settings?.searchProvider?.baseUrl ?? 'http://searxng:8080'} onChange={(event) => setSettings((prev: any) => ({ ...prev, searchProvider: { ...prev.searchProvider, baseUrl: event.target.value } }))} className={`${inputClass} md:col-span-2`} placeholder="SearXNG 服务地址，例如 http://searxng:8080" />
                    <input value={settings?.searchProvider?.resultCount ?? 5} onChange={(event) => setSettings((prev: any) => ({ ...prev, searchProvider: { ...prev.searchProvider, resultCount: Number(event.target.value) || 5 } }))} className={inputClass} type="number" min={1} max={20} placeholder="来源数量" />
                    <input value={settings?.searchProvider?.timeoutSeconds ?? 8} onChange={(event) => setSettings((prev: any) => ({ ...prev, searchProvider: { ...prev.searchProvider, timeoutSeconds: Number(event.target.value) || 8 } }))} className={inputClass} type="number" min={1} max={30} placeholder="超时时间（秒）" />
                  </div>
                  <button
                    onClick={async () => {
                      try {
                        await api.adminUpdateSearchProvider(settings?.searchProvider)
                        setNotice({ title: '联网检索配置已保存', body: '深度搜索会优先使用 OpenAI/Claude 或兼容端点的原生 Web Search；不可用时按新的 SearXNG 地址、来源数量和超时时间兜底。' })
                        await refresh()
                      } catch (error) {
                        setNotice({ title: '联网检索配置保存失败', body: error instanceof Error ? error.message : '联网检索配置保存失败，请稍后重试。' })
                      }
                    }}
                    className={`mt-4 px-4 py-2.5 rounded-xl text-sm font-medium ${colors.btnPrimary}`}
                  >
                    保存联网检索配置
                  </button>
                </div>
                <div className={`p-6 border-b ${colors.border}`}>
                  <div className="flex flex-col gap-4 md:flex-row md:items-center md:justify-between">
                    <div>
                      <h3 className="text-base font-medium mb-1">对话分享协作人数</h3>
                      <p className={`text-sm ${colors.textMuted}`}>控制各会员套餐在“分享聊天对话”时允许开启的最大协作人数。设为 0 表示该套餐不支持协作。</p>
                    </div>
                    <button onClick={() => void saveShareCollaborationConfig()} className={`px-4 py-2.5 rounded-xl text-sm font-medium ${colors.btnPrimary}`}>
                      保存协作人数
                    </button>
                  </div>
                  <div className="mt-5 grid grid-cols-1 gap-4 md:grid-cols-2 xl:grid-cols-5">
                    {MEMBERSHIP_PLAN_OPTIONS.map((plan) => (
                      <div key={`share-collaboration-${plan.code}`} className={`rounded-2xl border p-5 ${colors.border}`}>
                        <div className="text-base font-semibold">{plan.label}</div>
                        <div className={`mt-1 text-xs ${colors.textMuted}`}>当前套餐的最多协作人数</div>
                        <div className="mt-4">
                          <div className={`mb-2 text-xs font-medium ${colors.textMuted}`}>最大协作人数</div>
                          <input
                            value={settings?.shareCollaborationConfig?.[plan.code]?.maxCollaborators ?? ''}
                            onChange={(event) =>
                              setSettings((prev: any) => ({
                                ...(prev ?? {}),
                                shareCollaborationConfig: {
                                  ...(prev?.shareCollaborationConfig ?? {}),
                                  [plan.code]: {
                                    ...(prev?.shareCollaborationConfig?.[plan.code] ?? {}),
                                    maxCollaborators: Number(event.target.value) || 0,
                                  },
                                },
                              }))
                            }
                            className={inputClass}
                            type="number"
                            min={0}
                            step={1}
                          />
                        </div>
                      </div>
                    ))}
                  </div>
                </div>
                <div className="p-6">
                  <div className="flex flex-col gap-4 md:flex-row md:items-center md:justify-between">
                    <div>
                      <h3 className="text-base font-medium mb-1">OAuth 第三方登录配置</h3>
                      <p className={`text-sm ${colors.textMuted}`}>不限 GitHub / Google，任何标准 OAuth/OIDC 提供方都可以在这里新增并上传 logo。</p>
                    </div>
                    <button onClick={() => openOAuthEditor()} className={`px-4 py-2.5 rounded-xl text-sm font-medium ${colors.btnPrimary}`}>
                      <PlusCircle className="w-4 h-4 inline mr-2" />
                      新增 Provider
                    </button>
                  </div>
                  <div className="mt-6 space-y-4">
                    {(settings?.oauthProviders ?? []).map((provider: any) => (
                      <div key={provider.slug} className={`rounded-2xl border p-5 ${colors.border}`}>
                        <div className="flex flex-col gap-4 md:flex-row md:items-center md:justify-between">
                          <div className="flex items-center gap-4">
                            {provider.logoUrl ? (
                              <img src={provider.logoUrl} alt={provider.name} className="h-12 w-12 rounded-2xl object-cover border border-white/10" />
                            ) : (
                              <div className={`h-12 w-12 rounded-2xl flex items-center justify-center text-lg font-semibold ${isDark ? 'bg-white text-black' : 'bg-black text-white'}`}>
                                {provider.name?.slice(0, 1) || 'O'}
                              </div>
                            )}
                            <div>
                              <div className="flex items-center gap-2">
                                <span className="font-medium text-sm">{provider.name}</span>
                                <span className={`px-2 py-0.5 rounded-full text-[11px] font-mono border ${colors.border} ${colors.textMuted}`}>{provider.slug}</span>
                              </div>
                              <div className={`text-sm ${colors.textMuted}`}>{provider.scopes || '未配置 scope'}</div>
                            </div>
                          </div>
                          <div className="flex items-center gap-3">
                            <label className="flex items-center gap-2 text-sm">
                              <input type="checkbox" checked={provider.enabled} onChange={(event) => setSettings((prev: any) => ({ ...prev, oauthProviders: prev.oauthProviders.map((item: any) => item.slug === provider.slug ? { ...item, enabled: event.target.checked } : item) }))} />
                              已启用
                            </label>
                            <button onClick={() => openOAuthEditor(provider)} className={ghostButtonClass}>编辑</button>
                            <button onClick={async () => { await api.adminUpdateOAuth(provider.slug, provider); setMessage(`${provider.name} 状态已保存`); await refresh() }} className={`px-4 py-2.5 rounded-xl text-sm font-medium ${colors.btnPrimary}`}>保存状态</button>
                          </div>
                        </div>
                      </div>
                    ))}
                  </div>
                </div>
              </div>
            </div>
          )}
        </div>
      </main>

      {showModelConfig && activeModel && (
        <div className="fixed inset-0 bg-black/60 z-[100] flex items-center justify-center p-4 backdrop-blur-sm">
          <div className={`w-full max-w-3xl rounded-2xl shadow-2xl border flex flex-col max-h-[88vh] ${colors.cardBg} ${colors.border}`}>
            <div className={`p-5 border-b flex justify-between items-center ${colors.border}`}>
              <div>
                <h2 className="text-lg font-medium">{isCreatingModel ? '新增模型' : `${activeModel.name} 参数配置`}</h2>
                <p className={`text-xs mt-1 ${colors.textMuted}`}>配置用户端可见模型名称、上游模型、请求协议与端点编排策略。</p>
              </div>
              <button onClick={() => setShowModelConfig(false)} className={`p-1.5 rounded-md ${colors.hover}`}>
                <X className="w-5 h-5" />
              </button>
            </div>
            <div className="flex-1 overflow-y-auto p-6 space-y-6">
              <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
                <input value={activeModel.slug ?? ''} onChange={(event) => setActiveModel((prev: any) => ({ ...prev, slug: event.target.value }))} className={inputClass} placeholder="model slug" disabled={!isCreatingModel} />
                <input value={activeModel.name ?? ''} onChange={(event) => setActiveModel((prev: any) => ({ ...prev, name: event.target.value }))} className={inputClass} placeholder="显示名称" />
                <input value={activeModel.upstreamModel ?? ''} onChange={(event) => setActiveModel((prev: any) => ({ ...prev, upstreamModel: event.target.value }))} className={inputClass} placeholder="上游模型名，例如 gpt-4.1" />
                <select value={activeModel.modelType} onChange={(event) => setActiveModel((prev: any) => ({ ...prev, modelType: event.target.value }))} className={inputClass}>
                  <option value="chat">对话模型</option>
                  <option value="reasoning">推理模型</option>
                  <option value="vision">视觉模型</option>
                  <option value="embedding">向量模型</option>
                  <option value="image">图片生成模型</option>
                </select>
              </div>
              <textarea value={activeModel.description ?? ''} onChange={(event) => setActiveModel((prev: any) => ({ ...prev, description: event.target.value }))} className={`${inputClass} min-h-24`} placeholder="模型描述，会同步展示在用户端模型列表里" />
              <div className="grid grid-cols-1 md:grid-cols-[1fr_auto] gap-4 items-center">
                <select value={activeModel.protocol} onChange={(event) => setActiveModel((prev: any) => ({ ...prev, protocol: event.target.value }))} className={inputClass}>
                  <option value="openai">OpenAI 兼容协议</option>
                  <option value="anthropic">Anthropic (Claude) 协议</option>
                </select>
                <label className={`flex items-center justify-between gap-3 rounded-xl border px-4 py-3 ${colors.border}`}>
                  <span className="text-sm font-medium">对用户端可见</span>
                  <input type="checkbox" checked={Boolean(activeModel.active)} onChange={(event) => setActiveModel((prev: any) => ({ ...prev, active: event.target.checked }))} />
                </label>
              </div>
              <div className={`flex p-1 rounded-xl border ${colors.border} ${colors.inputBg}`}>
                <button onClick={() => setActiveModel((prev: any) => ({ ...prev, strategy: 'sequential' }))} className={`flex-1 text-sm py-1.5 rounded-sm ${activeModel.strategy === 'sequential' ? 'bg-[#333333] text-white' : colors.textMuted}`}>顺序轮询</button>
                <button onClick={() => setActiveModel((prev: any) => ({ ...prev, strategy: 'concurrent' }))} className={`flex-1 text-sm py-1.5 rounded-sm ${activeModel.strategy === 'concurrent' ? 'bg-[#333333] text-white' : colors.textMuted}`}>并发请求</button>
              </div>
              <div className={`rounded-2xl border p-5 space-y-4 ${colors.border}`}>
                <div className="flex flex-col gap-3 md:flex-row md:items-start md:justify-between">
                  <div>
                    <h3 className="text-sm font-semibold">模型提示词</h3>
                    <p className={`text-xs mt-1 ${colors.textMuted}`}>开启后，会在每次请求这个模型时自动注入系统提示词。</p>
                  </div>
                  <label className={`flex items-center justify-between gap-3 rounded-xl border px-4 py-3 ${colors.border}`}>
                    <span className="text-sm font-medium">启用提示词</span>
                    <input type="checkbox" checked={Boolean(activeModel.promptEnabled)} onChange={(event) => setActiveModel((prev: any) => ({ ...prev, promptEnabled: event.target.checked }))} />
                  </label>
                </div>
                <textarea
                  value={activeModel.promptText ?? ''}
                  onChange={(event) => setActiveModel((prev: any) => ({ ...prev, promptText: event.target.value }))}
                  className={`${inputClass} min-h-36 ${activeModel.promptEnabled ? '' : 'opacity-60'}`}
                  placeholder="输入这个模型的系统提示词，例如角色约束、输出格式、品牌语气、禁止项等。"
                />
              </div>
              <div className="space-y-3">
                {(Array.isArray(activeModel.endpoints) ? activeModel.endpoints : []).map((endpoint: any, idx: number) => (
                  <div key={idx} className="flex flex-col md:flex-row items-stretch gap-2">
                    <div className={`flex items-center px-2 rounded-xl border ${colors.inputBg} ${colors.border} flex-1`}>
                      <Link className={`w-4 h-4 ${colors.textMuted}`} />
                      <input value={endpoint.baseUrl} onChange={(event) => setActiveModel((prev: any) => ({ ...prev, endpoints: prev.endpoints.map((item: any, index: number) => index === idx ? { ...item, baseUrl: event.target.value } : item) }))} className={`w-full bg-transparent border-none outline-none text-sm p-2 ${colors.textMain}`} placeholder="https://api..." />
                    </div>
                    <div className={`flex items-center px-2 rounded-xl border ${colors.inputBg} ${colors.border} flex-1`}>
                      <Key className={`w-4 h-4 ${colors.textMuted}`} />
                      <input value={endpoint.secret ?? ''} onChange={(event) => setActiveModel((prev: any) => ({ ...prev, endpoints: prev.endpoints.map((item: any, index: number) => index === idx ? { ...item, secret: event.target.value } : item) }))} className={`w-full bg-transparent border-none outline-none text-sm p-2 ${colors.textMain}`} placeholder="sk-..." />
                    </div>
                    <label className={`flex items-center justify-between gap-3 rounded-xl border px-4 py-3 ${colors.border}`}>
                      <span className="text-xs font-medium">启用</span>
                      <input type="checkbox" checked={endpoint.active !== false} onChange={(event) => setActiveModel((prev: any) => ({ ...prev, endpoints: prev.endpoints.map((item: any, index: number) => index === idx ? { ...item, active: event.target.checked } : item) }))} />
                    </label>
                    <button onClick={() => setActiveModel((prev: any) => ({ ...prev, endpoints: prev.endpoints.filter((_: any, index: number) => index !== idx) }))} className="p-2 rounded-xl text-red-500 hover:bg-red-500/10">
                      <Trash2 className="w-4 h-4" />
                    </button>
                  </div>
                ))}
                <button onClick={() => setActiveModel((prev: any) => ({ ...prev, endpoints: [...prev.endpoints, { baseUrl: '', secret: '', active: true }] }))} className="flex items-center gap-1.5 text-sm font-medium text-blue-500">
                  <PlusCircle className="w-4 h-4" /> 添加新的代理端点
                </button>
              </div>
              <div className="space-y-3">
                <ModelProbePanel probe={activeModelProbeResult} colors={colors} />
                {Array.isArray(activeModelProbeResult?.endpoints) && activeModelProbeResult.endpoints.length > 0 && (
                  <div className={`rounded-2xl border overflow-hidden ${colors.border}`}>
                    {(activeModelProbeResult.endpoints ?? []).map((endpoint: any) => (
                      <div key={`${endpoint.index}-${endpoint.baseUrl}`} className={`flex flex-col gap-2 border-b px-4 py-3 last:border-b-0 md:flex-row md:items-center md:justify-between ${colors.border}`}>
                        <div className="min-w-0">
                          <div className="text-sm font-medium">端点 {endpoint.index}</div>
                          <div className={`truncate text-xs ${colors.textMuted}`}>{endpoint.baseUrl || '未填写地址'}</div>
                        </div>
                        <div className="flex items-center gap-3 text-xs">
                          <span className={`inline-flex rounded-full border px-2.5 py-1 font-medium ${probeBadgeClass(endpoint.status)}`}>{formatModelProbeStatus(endpoint.status)}</span>
                          <span className={colors.textMuted}>{endpoint.latencyMs ?? 0}ms</span>
                          <span className={`max-w-[320px] truncate ${colors.textMuted}`} title={endpoint.message || ''}>{endpoint.message || '-'}</span>
                        </div>
                      </div>
                    ))}
                  </div>
                )}
              </div>
            </div>
            <div className={`p-5 border-t flex justify-end gap-3 ${colors.border}`}>
              {!isCreatingModel && (
                <button onClick={() => void handleDeleteModel(activeModel)} className="px-4 py-2.5 text-sm font-medium rounded-xl border border-red-500/30 text-red-500 hover:bg-red-500/10">
                  删除模型
                </button>
              )}
              <button
                onClick={() => void handleProbeModel(activeModel, { fromModal: true })}
                disabled={probingModelKey === modelProbeKey(activeModel)}
                className={ghostButtonClass}
              >
                {probingModelKey === modelProbeKey(activeModel) ? '测试中...' : '一键测通路'}
              </button>
              <button onClick={() => setShowModelConfig(false)} className={ghostButtonClass}>取消</button>
              <button onClick={saveModelConfig} className={`px-4 py-2.5 text-sm font-medium rounded-xl ${colors.btnPrimary}`}>{isCreatingModel ? '创建模型' : '保存配置'}</button>
            </div>
          </div>
        </div>
      )}

      {showOAuthConfig && activeOAuthProvider && (
        <div className="fixed inset-0 bg-black/60 z-[110] flex items-center justify-center p-4 backdrop-blur-sm">
          <div className={`w-full max-w-4xl rounded-2xl shadow-2xl border flex flex-col max-h-[88vh] ${colors.cardBg} ${colors.border}`}>
            <div className={`p-5 border-b flex justify-between items-center ${colors.border}`}>
              <div>
                <h2 className="text-lg font-medium">{activeOAuthProvider.isNew ? '新增 OAuth Provider' : `编辑 ${activeOAuthProvider.name}`}</h2>
                <p className={`text-xs mt-1 ${colors.textMuted}`}>支持任意标准 OAuth/OIDC 提供方，logo 直接在后台上传。</p>
              </div>
              <button onClick={() => setShowOAuthConfig(false)} className={`p-1.5 rounded-md ${colors.hover}`}>
                <X className="w-5 h-5" />
              </button>
            </div>
            <div className="flex-1 overflow-y-auto p-6 space-y-6">
              <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
                <input value={activeOAuthProvider.slug ?? ''} onChange={(event) => setActiveOAuthProvider((prev: any) => ({ ...prev, slug: event.target.value }))} className={inputClass} placeholder="provider slug" disabled={!activeOAuthProvider.isNew} />
                <input value={activeOAuthProvider.name ?? ''} onChange={(event) => setActiveOAuthProvider((prev: any) => ({ ...prev, name: event.target.value }))} className={inputClass} placeholder="显示名称" />
                <input value={activeOAuthProvider.authUrl ?? ''} onChange={(event) => setActiveOAuthProvider((prev: any) => ({ ...prev, authUrl: event.target.value }))} className={inputClass} placeholder="Auth URL" />
                <input value={activeOAuthProvider.tokenUrl ?? ''} onChange={(event) => setActiveOAuthProvider((prev: any) => ({ ...prev, tokenUrl: event.target.value }))} className={inputClass} placeholder="Token URL" />
                <input value={activeOAuthProvider.userInfoUrl ?? ''} onChange={(event) => setActiveOAuthProvider((prev: any) => ({ ...prev, userInfoUrl: event.target.value }))} className={inputClass} placeholder="UserInfo URL" />
                <input value={activeOAuthProvider.scopes ?? ''} onChange={(event) => setActiveOAuthProvider((prev: any) => ({ ...prev, scopes: event.target.value }))} className={inputClass} placeholder="Scopes" />
                <input value={activeOAuthProvider.clientId ?? ''} onChange={(event) => setActiveOAuthProvider((prev: any) => ({ ...prev, clientId: event.target.value }))} className={inputClass} placeholder="Client ID" />
                <input value={activeOAuthProvider.clientSecret ?? ''} onChange={(event) => setActiveOAuthProvider((prev: any) => ({ ...prev, clientSecret: event.target.value }))} className={inputClass} placeholder="Client Secret" />
                <input value={activeOAuthProvider.userIdField ?? 'id'} onChange={(event) => setActiveOAuthProvider((prev: any) => ({ ...prev, userIdField: event.target.value }))} className={inputClass} placeholder="用户 ID 字段" />
                <input value={activeOAuthProvider.userEmailField ?? 'email'} onChange={(event) => setActiveOAuthProvider((prev: any) => ({ ...prev, userEmailField: event.target.value }))} className={inputClass} placeholder="邮箱字段" />
                <input value={activeOAuthProvider.userNameField ?? 'name'} onChange={(event) => setActiveOAuthProvider((prev: any) => ({ ...prev, userNameField: event.target.value }))} className={inputClass} placeholder="昵称字段" />
                <label className={`flex items-center justify-between rounded-xl border px-4 py-3 ${colors.border}`}>
                  <span className="text-sm font-medium">启用登录入口</span>
                  <input type="checkbox" checked={Boolean(activeOAuthProvider.enabled)} onChange={(event) => setActiveOAuthProvider((prev: any) => ({ ...prev, enabled: event.target.checked }))} />
                </label>
              </div>
              <div className={`rounded-2xl border p-4 ${colors.border}`}>
                <div className="flex flex-col gap-4 md:flex-row md:items-center">
                  {activeOAuthProvider.logoUrl ? (
                    <img src={activeOAuthProvider.logoUrl} alt={activeOAuthProvider.name} className="h-20 w-20 rounded-2xl object-cover border border-white/10" />
                  ) : (
                    <div className={`h-20 w-20 rounded-2xl flex items-center justify-center text-2xl font-semibold ${isDark ? 'bg-white text-black' : 'bg-black text-white'}`}>
                      {(activeOAuthProvider.name || 'O').slice(0, 1)}
                    </div>
                  )}
                  <div className="space-y-2">
                    <div className="text-sm font-medium">上传 Provider Logo</div>
                    <input type="file" accept="image/*" onChange={(event) => void handleOAuthLogoUpload(event.target.files?.[0] ?? null)} className="text-sm" />
                    <div className={`text-xs ${colors.textMuted}`}>建议使用正方形透明 PNG 或 SVG，前台登录页会直接展示这个 logo。</div>
                  </div>
                </div>
              </div>
              <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
                <textarea value={activeOAuthProvider.authParamsText ?? '{}'} onChange={(event) => setActiveOAuthProvider((prev: any) => ({ ...prev, authParamsText: event.target.value }))} className={`${inputClass} min-h-32`} placeholder="授权附加参数 JSON" />
                <textarea value={activeOAuthProvider.tokenParamsText ?? '{}'} onChange={(event) => setActiveOAuthProvider((prev: any) => ({ ...prev, tokenParamsText: event.target.value }))} className={`${inputClass} min-h-32`} placeholder="Token 附加参数 JSON" />
              </div>
            </div>
            <div className={`p-5 border-t flex justify-end gap-3 ${colors.border}`}>
              <button onClick={() => setShowOAuthConfig(false)} className={ghostButtonClass}>取消</button>
              <button onClick={() => void saveOAuthProvider()} className={`px-4 py-2.5 text-sm font-medium rounded-xl ${colors.btnPrimary}`}>{activeOAuthProvider.isNew ? '创建 Provider' : '保存 Provider'}</button>
            </div>
          </div>
        </div>
      )}

      {showMemberConfig && activeMember && (
        <div className="fixed inset-0 bg-black/60 z-[100] flex items-center justify-center p-4 backdrop-blur-sm">
          <div className={`w-full max-w-lg rounded-xl shadow-2xl border flex flex-col max-h-[90vh] overflow-hidden ${colors.cardBg} ${colors.border}`}>
            <div className={`p-5 border-b flex justify-between items-center ${colors.border}`}>
              <h2 className="text-lg font-medium">用户与订阅详情</h2>
              <button onClick={() => setShowMemberConfig(false)} className={`p-1.5 rounded-md ${colors.hover}`}>
                <X className="w-5 h-5" />
              </button>
            </div>
            <div className="p-6 space-y-6 overflow-y-auto">
              <div className="space-y-4">
                <select value={activeMember.rawPlan} onChange={(event) => setActiveMember((prev: any) => ({ ...prev, rawPlan: event.target.value }))} className={`w-full px-3 py-2 rounded-md border ${colors.inputBg} ${colors.border}`}>
                  {MEMBERSHIP_PLAN_OPTIONS.map((option) => (
                    <option key={option.code} value={option.code}>{option.label}</option>
                  ))}
                </select>
                <input value={activeMember.expiryDate ?? ''} onChange={(event) => setActiveMember((prev: any) => ({ ...prev, expiryDate: event.target.value }))} className={`w-full px-3 py-2 rounded-md border ${colors.inputBg} ${colors.border}`} type="date" />
	                <select value={activeMember.statusCode ?? 'active'} onChange={(event) => setActiveMember((prev: any) => ({ ...prev, statusCode: event.target.value, status: formatUserStatus(event.target.value) }))} className={`w-full px-3 py-2 rounded-md border ${colors.inputBg} ${colors.border}`}>
	                  <option value="active">正常</option>
	                  <option value="suspended">已停用</option>
	                  <option value="banned">已封禁</option>
	                </select>
	                <div className={`rounded-2xl border p-4 ${colors.border}`}>
	                  <div className="text-sm font-medium">账号级上下文长度覆盖</div>
	                  <div className={`mt-1 text-xs leading-5 ${colors.textMuted}`}>留空则继承套餐配置；填写后只覆盖这个账号在对应模型上的上下文输入长度。</div>
	                  <div className="mt-4 space-y-3">
	                    {chatModels.map((model) => (
	                      <label key={`member-context-${model.slug}`} className="block">
	                        <span className={`mb-1 block text-xs ${colors.textMuted}`}>{model.name || model.slug}</span>
	                        <input
	                          value={activeMember.contextLimits?.[model.slug] ?? ''}
	                          onChange={(event) => setActiveMember((prev: any) => ({
	                            ...prev,
	                            contextLimits: {
	                              ...(prev?.contextLimits ?? {}),
	                              [model.slug]: event.target.value,
	                            },
	                          }))}
	                          className={`w-full px-3 py-2 rounded-md border ${colors.inputBg} ${colors.border}`}
	                          type="number"
	                          min={256}
	                          step={256}
	                          placeholder="继承套餐上下文长度"
	                        />
	                      </label>
	                    ))}
	                  </div>
	                </div>
	              </div>
            </div>
            <div className={`p-5 border-t flex justify-end gap-3 ${colors.border}`}>
              <button onClick={() => setShowMemberConfig(false)} className={`px-4 py-2 text-sm font-medium rounded-md border ${colors.border}`}>取消</button>
              <button onClick={saveMemberConfig} className={`px-4 py-2 text-sm font-medium rounded-md ${colors.btnPrimary}`}>保存更改</button>
            </div>
          </div>
        </div>
      )}

      {showGiftModal && (
        <div className="fixed inset-0 bg-black/60 z-[100] flex items-center justify-center p-4 backdrop-blur-sm">
          <div className={`w-full max-w-md rounded-xl shadow-2xl border flex flex-col ${colors.cardBg} ${colors.border}`}>
            <div className={`p-5 border-b flex justify-between items-center ${colors.border}`}>
              <h2 className="text-lg font-medium">生成礼品卡兑换链接</h2>
              <button onClick={() => { setShowGiftModal(false); setGiftStep(1) }} className={`p-1.5 rounded-md ${colors.hover}`}>
                <X className="w-5 h-5" />
              </button>
            </div>
            {giftStep === 1 ? (
              <div className="p-6 space-y-6">
                <select value={giftParams.planCode} onChange={(event) => setGiftParams((prev) => ({ ...prev, planCode: event.target.value }))} className={`w-full px-3 py-2.5 rounded-md border ${colors.inputBg} ${colors.border}`}>
                  <option value="go">Go 版</option>
                  <option value="plus">Plus 版</option>
                  <option value="pro_basic">Pro 版 (基础档)</option>
                  <option value="pro_max">Pro 版 (满血档)</option>
                </select>
                <input value={giftParams.duration} onChange={(event) => setGiftParams((prev) => ({ ...prev, duration: Number(event.target.value) }))} type="number" className={`w-full px-3 py-2.5 rounded-md border ${colors.inputBg} ${colors.border}`} placeholder="月数" />
                <input value={giftParams.maxUses} onChange={(event) => setGiftParams((prev) => ({ ...prev, maxUses: Number(event.target.value) }))} type="number" className={`w-full px-3 py-2.5 rounded-md border ${colors.inputBg} ${colors.border}`} placeholder="可兑换次数" />
                <input value={giftParams.expiryDate} onChange={(event) => setGiftParams((prev) => ({ ...prev, expiryDate: event.target.value }))} type="date" className={`w-full px-3 py-2.5 rounded-md border ${colors.inputBg} ${colors.border}`} />
                <div className="space-y-2">
                  <label className={`flex items-center gap-3 p-3 border rounded-md ${colors.border}`}>
                    <input type="radio" checked={giftParams.accountType === 'has_account'} onChange={() => setGiftParams((prev) => ({ ...prev, accountType: 'has_account' }))} />
                    <span className="text-sm font-medium">仅限已有账号充值</span>
                  </label>
                  <label className={`flex items-center gap-3 p-3 border rounded-md ${colors.border}`}>
                    <input type="radio" checked={giftParams.accountType === 'no_account'} onChange={() => setGiftParams((prev) => ({ ...prev, accountType: 'no_account' }))} />
                    <span className="text-sm font-medium">允许无账号用户注册并绑定</span>
                  </label>
                </div>
                <button onClick={() => void handleGenerateGift()} className={`w-full py-2.5 text-sm font-medium rounded-md ${colors.btnPrimary}`}>生成兑换链接</button>
              </div>
            ) : (
              <div className="p-6 flex flex-col items-center text-center">
                <Gift className="w-8 h-8 text-green-500 mb-4" />
                <h3 className="text-lg font-medium mb-2">生成成功</h3>
                <input readOnly value={giftLink} className={`w-full p-3 rounded-md border mb-6 ${colors.inputBg} ${colors.border}`} />
                <div className="w-full space-y-3">
                  <a href={giftLink} className={`block w-full py-2.5 text-sm font-medium rounded-md text-center ${colors.btnPrimary}`}>
                    打开兑换页
                  </a>
                  <button onClick={() => void copyInviteValue(giftLink)} className={`w-full py-2.5 text-sm font-medium rounded-md border ${colors.border} ${colors.hover}`}>
                    {copiedInviteValue === giftLink ? '已复制兑换链接' : '复制兑换链接'}
                  </button>
                </div>
              </div>
            )}
          </div>
        </div>
      )}

      {activeServiceAlert && (
        <div className="fixed right-6 top-6 z-[130] w-full max-w-md">
          <div className={`flex max-h-[calc(100vh-3rem)] flex-col overflow-hidden rounded-2xl border px-5 py-4 shadow-2xl ${colors.cardBg} ${colors.border}`}>
            <div className="flex shrink-0 items-start justify-between gap-4">
              <div>
                <div className="text-sm font-semibold text-red-500">后台异常提醒</div>
                <div className="mt-1 text-base font-medium">{formatServiceAlertSource(activeServiceAlert.source)}</div>
              </div>
              <span className={`inline-flex rounded-full px-2.5 py-1 text-xs font-medium border ${serviceAlertBadgeClass(activeServiceAlert.status)}`}>
                {formatServiceAlertStatus(activeServiceAlert.status)}
              </span>
            </div>
            <div className="mt-4 min-h-0 flex-1 space-y-3 overflow-y-auto pr-1 text-sm">
              <div>
                <div className={colors.textMuted}>账号</div>
                <div className="mt-1 font-medium">{activeServiceAlert.account || activeServiceAlert.userId || '未知账号'}</div>
              </div>
              <div>
                <div className={colors.textMuted}>报错时间</div>
                <div className="mt-1">{activeServiceAlert.createdAt}</div>
              </div>
              <div>
                <div className={colors.textMuted}>报错内容</div>
                <div
                  className={`mt-1 whitespace-pre-wrap break-words rounded-xl border px-3 py-2 text-xs leading-6 ${colors.border} ${
                    serviceAlertShouldCollapse && !serviceAlertExpanded ? 'max-h-36 overflow-hidden' : 'max-h-[28vh] overflow-auto'
                  }`}
                >
                  {serviceAlertShouldCollapse && !serviceAlertExpanded ? serviceAlertPreview : serviceAlertDetail}
                </div>
                {serviceAlertShouldCollapse && (
                  <button
                    type="button"
                    onClick={() => setServiceAlertExpanded((prev) => !prev)}
                    className={`mt-2 inline-flex items-center gap-1 text-xs font-medium ${colors.textMuted} ${colors.hover}`}
                  >
                    <span>{serviceAlertExpanded ? '收起报错内容' : '展开完整报错'}</span>
                    <ChevronDown className={`h-3.5 w-3.5 transition-transform ${serviceAlertExpanded ? 'rotate-180' : ''}`} />
                  </button>
                )}
              </div>
            </div>
            <div className="mt-5 flex shrink-0 flex-wrap items-center justify-end gap-2">
              <button
                onClick={() => void handleCopyServiceAlert(activeServiceAlert)}
                className={`px-4 py-2 text-sm font-medium rounded-xl border ${colors.border} ${colors.hover}`}
              >
                {copiedServiceAlertID === activeServiceAlert.id ? '已复制' : '复制完整报错'}
              </button>
              <button
                onClick={() => navigate('/admin/after-sales')}
                className={`px-4 py-2 text-sm font-medium rounded-xl border ${colors.border} ${colors.hover}`}
              >
                查看售后服务
              </button>
              <button
                onClick={() => void handleReadServiceAlert(activeServiceAlert.id)}
                className={`px-4 py-2 text-sm font-medium rounded-xl ${colors.btnPrimary}`}
              >
                已读
              </button>
            </div>
          </div>
        </div>
      )}

      {notice && (
        <div className="fixed right-6 top-6 z-[120] w-full max-w-sm">
          <div className={`rounded-2xl border px-5 py-4 shadow-2xl ${colors.cardBg} ${colors.border}`}>
            <div className="text-sm font-semibold">{notice.title}</div>
            <div
              className={`mt-1 text-sm ${colors.textMuted} ${
                noticeShouldCollapse && !noticeExpanded ? 'line-clamp-3 whitespace-pre-wrap break-words' : 'whitespace-pre-wrap break-words'
              }`}
            >
              {notice.body}
            </div>
            {noticeShouldCollapse && (
              <button
                type="button"
                onClick={() => setNoticeExpanded((prev) => !prev)}
                className={`mt-3 inline-flex items-center gap-1 text-xs font-medium ${colors.textMuted} ${colors.hover}`}
              >
                <span>{noticeExpanded ? '收起' : '展开'}</span>
                <ChevronDown className={`h-3.5 w-3.5 transition-transform ${noticeExpanded ? 'rotate-180' : ''}`} />
              </button>
            )}
          </div>
        </div>
      )}
    </div>
  )
}

function parseLooseJSON(value: string) {
  try {
    const parsed = JSON.parse(value || '{}')
    if (parsed && typeof parsed === 'object' && !Array.isArray(parsed)) {
      return parsed
    }
  } catch {
    // Invalid custom JSON should fall back to an empty object.
  }
  return {}
}

function modelProbeKey(model: any) {
  return String(model?.slug || model?.upstreamModel || model?.name || 'draft-model')
}

function countActiveEndpoints(model: any) {
  if (!Array.isArray(model?.endpoints)) {
    return 0
  }
  return model.endpoints.filter((endpoint: any) => endpoint?.active !== false).length
}

function formatModelProbeStatus(status: string) {
  switch (status) {
    case 'healthy':
      return '通路正常'
    case 'degraded':
      return '部分可用'
    case 'failed':
      return '检测失败'
    case 'success':
      return '检测成功'
    case 'skipped':
      return '已跳过'
    default:
      return '待检测'
  }
}

function probeBarClass(status: string) {
  switch (status) {
    case 'healthy':
    case 'success':
      return 'bg-green-500'
    case 'degraded':
      return 'bg-amber-500'
    case 'failed':
      return 'bg-red-500'
    default:
      return 'bg-slate-400'
  }
}

function probeBadgeClass(status: string) {
  switch (status) {
    case 'healthy':
    case 'success':
      return 'border-green-500/30 bg-green-500/10 text-green-500'
    case 'degraded':
      return 'border-amber-500/30 bg-amber-500/10 text-amber-500'
    case 'failed':
      return 'border-red-500/30 bg-red-500/10 text-red-500'
    default:
      return 'border-slate-500/30 bg-slate-500/10 text-slate-500'
  }
}

function ModelProbePanel({ probe, colors }: { probe: any; colors: any }) {
  if (!probe?.summary) {
    return (
      <div className={`rounded-2xl border p-4 ${colors.border}`}>
        <div className="flex items-center justify-between gap-3">
          <div>
            <div className="text-sm font-medium">通路状态</div>
            <div className={`mt-1 text-xs ${colors.textMuted}`}>尚未测试，点击一键测通路开始检测。</div>
          </div>
          <span className={`inline-flex rounded-full border px-2.5 py-1 text-xs font-medium ${probeBadgeClass('idle')}`}>待检测</span>
        </div>
        <div className={`mt-3 h-2 w-full rounded-full ${colors.inputBg}`}>
          <div className="h-2 w-0 rounded-full bg-slate-400" />
        </div>
      </div>
    )
  }
  const summary = probe.summary
  const successRate = Math.max(0, Math.min(100, Number(summary.successRate ?? 0)))
  return (
    <div className={`rounded-2xl border p-4 ${colors.border}`}>
      <div className="flex flex-col gap-3 md:flex-row md:items-start md:justify-between">
        <div>
          <div className="text-sm font-medium">通路状态</div>
          <div className={`mt-1 text-xs ${colors.textMuted}`}>{summary.message || '最近一次检测结果'}</div>
        </div>
        <div className="text-right">
          <span className={`inline-flex rounded-full border px-2.5 py-1 text-xs font-medium ${probeBadgeClass(summary.status)}`}>{formatModelProbeStatus(summary.status)}</span>
          <div className="mt-2 text-2xl font-semibold">{successRate}%</div>
        </div>
      </div>
      <div className={`mt-3 h-2 w-full rounded-full ${colors.inputBg}`}>
        <div className={`h-2 rounded-full ${probeBarClass(summary.status)}`} style={{ width: `${successRate}%` }} />
      </div>
      <div className={`mt-3 grid grid-cols-2 gap-3 text-xs ${colors.textMuted} md:grid-cols-4`}>
        <div>成功端点 {summary.successfulEndpoints ?? 0}</div>
        <div>启用端点 {summary.activeEndpoints ?? 0}</div>
        <div>平均延迟 {summary.avgLatencyMs ?? 0}ms</div>
        <div>{probe.checkedAt || '刚刚检测'}</div>
      </div>
    </div>
  )
}

function normalizeModelMembershipLimitPayload(value: any) {
  const out: Record<string, Record<string, number>> = {}
  if (!value || typeof value !== 'object' || Array.isArray(value)) {
    return out
  }
  for (const [planCode, models] of Object.entries(value)) {
    if (!models || typeof models !== 'object' || Array.isArray(models)) {
      continue
    }
    const nextModels: Record<string, number> = {}
    for (const [modelSlug, rawLimit] of Object.entries(models as Record<string, unknown>)) {
      if (rawLimit === '' || rawLimit === null || rawLimit === undefined) {
        continue
      }
      const limit = Number(rawLimit)
      if (!Number.isFinite(limit) || limit < 0) {
        continue
      }
      nextModels[modelSlug] = Math.floor(limit)
    }
    out[planCode] = nextModels
  }
  return out
}

function normalizeModelContextLimitPayload(value: any) {
  const out: { default: number; models: Record<string, number>; plans: Record<string, Record<string, number>>; users: Record<string, Record<string, number>> } = {
    default: Math.max(0, Math.floor(Number(value?.default ?? 0) || 0)),
    models: {},
    plans: {},
    users: {},
  }
  if (value?.models && typeof value.models === 'object' && !Array.isArray(value.models)) {
    for (const [modelSlug, rawLimit] of Object.entries(value.models as Record<string, unknown>)) {
      const limit = Number(rawLimit)
      if (modelSlug && Number.isFinite(limit) && limit > 0) {
        out.models[modelSlug] = Math.floor(limit)
      }
    }
  }
  const normalizeNested = (source: any) => {
    const nestedOut: Record<string, Record<string, number>> = {}
    if (!source || typeof source !== 'object' || Array.isArray(source)) {
      return nestedOut
    }
    for (const [outerKey, inner] of Object.entries(source)) {
      if (!inner || typeof inner !== 'object' || Array.isArray(inner)) {
        continue
      }
      const nextInner: Record<string, number> = {}
      for (const [modelSlug, rawLimit] of Object.entries(inner as Record<string, unknown>)) {
        if (rawLimit === '' || rawLimit === null || rawLimit === undefined) {
          continue
        }
        const limit = Number(rawLimit)
        if (!Number.isFinite(limit) || limit <= 0) {
          continue
        }
        nextInner[modelSlug] = Math.floor(limit)
      }
      if (Object.keys(nextInner).length > 0) {
        nestedOut[outerKey] = nextInner
      }
    }
    return nestedOut
  }
  out.plans = normalizeNested(value?.plans)
  out.users = normalizeNested(value?.users)
  return out
}

function normalizeInfiniteCodeQuotaPayload(value: any) {
  const out: Record<string, { credits: number; resetHours: number }> = {}
  if (!value || typeof value !== 'object' || Array.isArray(value)) {
    return out
  }
  for (const [planCode, rawConfig] of Object.entries(value)) {
    if (!rawConfig || typeof rawConfig !== 'object' || Array.isArray(rawConfig)) {
      continue
    }
    const config = rawConfig as Record<string, unknown>
    out[planCode] = {
      credits: Math.max(0, Math.floor(Number(config.credits ?? 0) || 0)),
      resetHours: Math.max(1, Math.floor(Number(config.resetHours ?? 24) || 24)),
    }
  }
  return out
}

function normalizeShareCollaborationPayload(value: any) {
  const out: Record<string, { maxCollaborators: number }> = {}
  if (!value || typeof value !== 'object' || Array.isArray(value)) {
    return out
  }
  for (const [planCode, rawConfig] of Object.entries(value)) {
    if (!rawConfig || typeof rawConfig !== 'object' || Array.isArray(rawConfig)) {
      continue
    }
    const config = rawConfig as Record<string, unknown>
    out[planCode] = {
      maxCollaborators: Math.max(0, Math.floor(Number(config.maxCollaborators ?? 0) || 0)),
    }
  }
  return out
}

function getAdjacentPlan(planCode: string, direction: 'upgrade' | 'downgrade') {
  const currentIndex = MEMBERSHIP_PLAN_OPTIONS.findIndex((option) => option.code === planCode)
  const safeIndex = currentIndex >= 0 ? currentIndex : 0
  const nextIndex = direction === 'upgrade' ? safeIndex + 1 : safeIndex - 1
  return MEMBERSHIP_PLAN_OPTIONS[nextIndex] ?? null
}

function formatUserStatus(status: string) {
  switch (status) {
    case 'deleted':
      return '已删除'
    case 'banned':
      return '已封禁'
    case 'suspended':
      return '已停用'
    default:
      return '正常'
  }
}

function userStatusBadgeClass(status: string) {
  switch (status) {
    case 'banned':
      return 'border-red-500/30 text-red-500 bg-red-500/10'
    case 'suspended':
      return 'border-amber-500/30 text-amber-500 bg-amber-500/10'
    default:
      return 'border-green-500/30 text-green-500 bg-green-500/10'
  }
}

function formatModelAvailability(active: boolean) {
  return active ? '已启用' : '已停用'
}

function formatModelType(modelType: string) {
  switch (modelType) {
    case 'reasoning':
      return '推理模型'
    case 'vision':
      return '视觉模型'
    case 'embedding':
      return '向量模型'
    case 'image':
      return '图片生成模型'
    default:
      return '对话模型'
  }
}

function formatProtocol(protocol: string) {
  switch (protocol) {
    case 'anthropic':
      return 'Anthropic'
    default:
      return 'OpenAI 兼容'
  }
}

function formatStrategy(strategy: string) {
  switch (strategy) {
    case 'concurrent':
      return '并发请求'
    default:
      return '顺序轮询'
  }
}

function formatAPIStatus(status: string) {
  switch (status) {
    case 'ok':
      return '成功'
    case 'upstream_disconnected':
      return '上游断联'
    case 'upstream_failed':
      return '上游异常'
    case 'forbidden':
      return '已拒绝'
    case 'rate_limited':
      return '已限流'
    case 'error':
      return '失败'
    default:
      return status || '-'
  }
}

function formatAPIErrorDetail(detail: string) {
  const value = formatUserVisibleErrorDetail(detail)
  if (!value) {
    return '-'
  }
  return value.length > 72 ? `${value.slice(0, 72)}...` : value
}

function formatUserVisibleErrorDetail(detail: unknown, fallback = '-') {
  const value = String(detail ?? '').trim()
  if (!value) {
    return fallback
  }
  const lower = value.toLowerCase()
  if (lower.includes('not allowed') || lower.includes('forbidden')) return '当前账号无权执行该操作'
  if (lower.includes('captcha answer is incorrect') || lower.includes('captcha')) return '图形验证码不正确'
  if (lower.includes('context deadline exceeded') || lower.includes('deadline') || lower.includes('timeout') || lower.includes('failed to fetch') || lower.includes('connection refused') || lower.includes('no such host') || lower.includes('eof')) return '与服务器断联，请重试'
  if (lower.includes('email or phone') && lower.includes('password')) return '邮箱、手机号或密码不正确'
  if (lower.includes('invalid api key') || lower.includes('api key is invalid')) return 'API Key 无效或已被撤销'
  if (lower.includes('api key is required')) return '请先提供 API Key'
  if (lower.includes('rate limit') || lower.includes('too many requests')) return '请求过于频繁，请稍后再试'
  if (lower.includes('not found') || lower.includes('no rows')) return '内容不存在或已被删除'
  if (lower.includes('duplicate key') || lower.includes('unique constraint')) return '数据已存在，请检查后重试'
  if (lower.includes('invalid input syntax') || lower.includes('invalid uuid') || lower.includes('bad request')) return '请求参数不正确，请检查后重试'
  if (lower.includes('no active upstream endpoint') || lower.includes('provider route') || lower.includes('not configured')) return '模型上游端点未配置或未启用，请到后台模型管理检查配置'
  if (lower.includes('upstream') || lower.includes('openai-compatible') || lower.includes('anthropic')) return '上游模型返回异常，请检查模型配置或稍后重试'
  if (/^[A-Za-z0-9_ .:/?&="'{}\\[\\],()-]+$/.test(value) && !/[一-龥]/.test(value)) return '操作失败，请稍后重试'
  return value
}

function formatAdminSectionName(section: string) {
  switch (section) {
    case 'dashboard':
      return '仪表盘'
    case 'users':
      return '用户管理'
    case 'invite-links':
      return '邀请链接'
    case 'models':
      return '模型管理'
    case 'service-alerts':
      return '售后异常'
    case 'api-stats':
      return '接口统计'
    case 'system-logs':
      return '系统日志'
    case 'member-stats':
      return '会员记录'
    case 'finance':
      return '财务配置'
    case 'settings':
      return '系统设置'
    default:
      return '后台数据'
  }
}

function formatSystemLogTime(value: string) {
  if (!value) {
    return '-'
  }
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) {
    return value
  }
  return date.toLocaleString('zh-CN', { hour12: false })
}

function formatSystemLogService(value: string) {
  switch (value) {
    case 'bff':
      return 'BFF'
    case 'core':
      return 'Core'
    default:
      return value || '-'
  }
}

function formatSystemLogCategory(value: string) {
  switch (value) {
    case 'auth':
      return '认证'
    case 'payment':
      return '支付'
    case 'request':
      return '请求'
    default:
      return value || '-'
  }
}

function formatSystemLogStatus(statusCode: number, level: string) {
  if (Number(statusCode) > 0) {
    return String(statusCode)
  }
  switch (level) {
    case 'error':
      return '错误'
    case 'warn':
      return '警告'
    default:
      return '正常'
  }
}

function systemLogBadgeClass(level: string, statusCode: number) {
  if (Number(statusCode) >= 500 || level === 'error') {
    return 'border-red-500/30 text-red-500 bg-red-500/10'
  }
  if (Number(statusCode) >= 400 || level === 'warn') {
    return 'border-amber-500/30 text-amber-500 bg-amber-500/10'
  }
  return 'border-green-500/30 text-green-500 bg-green-500/10'
}

function getSystemLogCode(payload: any) {
  const value = typeof payload?.code === 'string' || typeof payload?.code === 'number'
    ? String(payload.code).trim()
    : ''
  return value || ''
}

function getSystemLogDeliveryMode(payload: any) {
  switch (String(payload?.deliveryMode || '').trim()) {
    case 'test':
      return '调试模式'
    case 'sms':
      return '短信'
    case 'email':
      return '邮箱'
    case 'preview':
      return '预览'
    default:
      return '-'
  }
}

function formatSystemLogPayload(payload: any) {
  if (!payload || typeof payload !== 'object') {
    return '-'
  }
  try {
    return JSON.stringify(payload, null, 2)
  } catch {
    return '-'
  }
}

function formatServiceAlertStatus(status: string) {
  switch (status) {
    case 'unread':
      return '未读'
    case 'read':
      return '已读待处理'
    case 'resolved':
      return '已处理'
    default:
      return status || '-'
  }
}

function serviceAlertBadgeClass(status: string) {
  switch (status) {
    case 'unread':
      return 'border-red-500/30 text-red-500 bg-red-500/10'
    case 'read':
      return 'border-amber-500/30 text-amber-500 bg-amber-500/10'
    case 'resolved':
      return 'border-green-500/30 text-green-500 bg-green-500/10'
    default:
      return 'border-slate-500/30 text-slate-500 bg-slate-500/10'
  }
}

function formatServiceAlertSource(source: string) {
  switch (source) {
    case 'web_chat':
      return '用户端聊天'
    case 'developer_api':
      return '开发者 API'
    default:
      return source || '未知来源'
  }
}

function formatServiceAlertCopyText(alert: any) {
  const lines = [
    `账号：${alert?.account || alert?.userId || '未知账号'}`,
    `模型：${alert?.model || '-'}`,
    `报错时间：${alert?.createdAt || '-'}`,
    `来源：${formatServiceAlertSource(String(alert?.source || ''))}`,
    `路径：${alert?.path || '-'}`,
    `状态：${formatServiceAlertStatus(String(alert?.status || ''))}`,
  ]
  if (alert?.userId) {
    lines.push(`用户 ID：${alert.userId}`)
  }
  if (alert?.keyId) {
    lines.push(`Key ID：${alert.keyId}`)
  }
  if (typeof alert?.latencyMs === 'number' && alert.latencyMs > 0) {
    lines.push(`耗时：${alert.latencyMs}ms`)
  }
  if (alert?.readAt) {
    lines.push(`已读时间：${alert.readAt}`)
  }
  if (alert?.resolvedAt) {
    lines.push(`处理时间：${alert.resolvedAt}`)
  }
  if (alert?.resolvedBy) {
    lines.push(`处理人：${alert.resolvedBy}`)
  }
  lines.push('', '完整报错：', alert?.errorDetail || '未返回错误详情')
  return lines.join('\n')
}

function formatAdminRole(role: string) {
  switch (role) {
    case 'super_admin':
      return '超级管理员'
    case 'ops_admin':
      return '运营管理员'
    case 'finance_admin':
      return '财务管理员'
    case 'support_admin':
      return '客服管理员'
    default:
      return role || '-'
  }
}

function canAccessAdminSection(role: string, section: string) {
  const normalizedRole = String(role || '')
  if (normalizedRole === 'super_admin') {
    return true
  }
  const allowedRoles: Record<string, string[]> = {
    dashboard: ['ops_admin'],
    users: ['support_admin', 'finance_admin'],
    'invite-links': ['support_admin'],
    models: ['ops_admin'],
    'service-alerts': ['ops_admin', 'support_admin'],
    'api-stats': ['ops_admin'],
    'system-logs': ['ops_admin', 'support_admin', 'finance_admin'],
    'member-stats': ['support_admin', 'finance_admin'],
    membership: ['ops_admin', 'support_admin', 'finance_admin'],
    finance: ['finance_admin'],
    'finance-management': ['finance_admin'],
    settings: ['ops_admin', 'support_admin', 'finance_admin'],
  }
  return (allowedRoles[section] ?? []).includes(normalizedRole)
}

function isIgnorableAdminLoadError(error: unknown) {
  if (!error || typeof error !== 'object') {
    return false
  }
  const status = Number((error as { status?: number }).status ?? 0)
  return status === 403 || status === 405
}

async function readFileAsDataURL(file: File) {
  return new Promise<string>((resolve, reject) => {
    const reader = new FileReader()
    reader.onload = () => resolve(typeof reader.result === 'string' ? reader.result : '')
    reader.onerror = () => reject(reader.error)
    reader.readAsDataURL(file)
  })
}
