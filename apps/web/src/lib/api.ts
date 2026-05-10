let csrfToken = ''
let cachedDeviceFingerprint = ''

type OAuthProviderSummary = {
  slug: string
  name: string
  logoUrl?: string
}

type ChatModelSummary = {
  slug: string
  label?: string
  name?: string
  desc?: string
  description?: string
  endpoints?: unknown[]
}

type FetchOptions = RequestInit & {
  skipJson?: boolean
}

type RequestOptions = {
  signal?: AbortSignal
}

function isAdminSurface() {
  if (typeof window === 'undefined') {
    return false
  }
  return window.location.pathname.startsWith('/admin') || window.location.port === '1003'
}

function resolveSessionCSRFToken(session: { csrfToken?: string; userCsrfToken?: string; adminCsrfToken?: string }) {
  if (isAdminSurface() && session.adminCsrfToken) {
    return session.adminCsrfToken
  }
  if (!isAdminSurface() && session.userCsrfToken) {
    return session.userCsrfToken
  }
  return session.csrfToken || session.userCsrfToken || session.adminCsrfToken || ''
}

function asArray<T>(value: unknown): T[] {
  return Array.isArray(value) ? (value as T[]) : []
}

function asObject<T extends Record<string, unknown>>(value: unknown, fallback: T): T {
  if (value && typeof value === 'object' && !Array.isArray(value)) {
    return { ...fallback, ...(value as Partial<T>) }
  }
  return fallback
}

function resolveDeviceFingerprint() {
  if (cachedDeviceFingerprint) {
    return cachedDeviceFingerprint
  }
  if (typeof window === 'undefined') {
    return ''
  }
  const nav = window.navigator
  const screenInfo = window.screen
  const parts = [
    nav.userAgent,
    nav.language,
    nav.platform,
    String(nav.hardwareConcurrency ?? ''),
    String(screenInfo?.width ?? ''),
    String(screenInfo?.height ?? ''),
    String(screenInfo?.colorDepth ?? ''),
    Intl.DateTimeFormat().resolvedOptions().timeZone,
  ]
  const raw = parts.join('|')
  try {
    cachedDeviceFingerprint = window.btoa(unescape(encodeURIComponent(raw))).replace(/=+$/g, '').slice(0, 180)
  } catch {
    cachedDeviceFingerprint = raw.slice(0, 180)
  }
  return cachedDeviceFingerprint
}

function toChineseErrorMessage(value: unknown, fallback = '操作失败，请稍后重试') {
  const text = String(value ?? '').trim()
  if (!text) return fallback
  const lower = text.toLowerCase()
  if (lower.includes('not allowed') || lower.includes('forbidden')) return '当前账号无权执行该操作'
  if (lower.includes('captcha answer is incorrect') || lower.includes('captcha') || lower.includes('验证码')) return text.includes('验证码') ? text : '图形验证码不正确'
  if (lower.includes('context deadline exceeded') || lower.includes('deadline') || lower.includes('timeout') || lower.includes('network') || lower.includes('failed to fetch')) return '与服务器断联，请重试'
  if (lower.includes('email or phone') && lower.includes('password')) return '邮箱、手机号或密码不正确'
  if (lower.includes('api key is required')) return '请先提供 API Key'
  if (lower.includes('api key is invalid')) return 'API Key 无效或已被撤销'
  if (lower.includes('rate limit')) return '请求过于频繁，请稍后再试'
  if (/^[A-Za-z0-9_ .:/-]+$/.test(text) && !/[一-龥]/.test(text)) return fallback
  return text
}

async function request<T>(input: string, init: FetchOptions = {}): Promise<T> {
  const headers = new Headers(init.headers ?? {})
  headers.set('Accept', 'application/json')
  headers.set('X-Infinite-API', '1')
  const deviceFingerprint = resolveDeviceFingerprint()
  if (deviceFingerprint) {
    headers.set('X-Device-Fingerprint', deviceFingerprint)
  }
  if (!headers.has('Content-Type') && init.body && !(init.body instanceof FormData)) {
    headers.set('Content-Type', 'application/json')
  }
  if (init.method && !['GET', 'HEAD'].includes(init.method.toUpperCase()) && csrfToken) {
    headers.set('X-CSRF-Token', csrfToken)
  }
  const response = await fetch(input, {
    cache: 'no-store',
    credentials: 'include',
    ...init,
    headers,
  })
  if (!response.ok) {
    const payload = await response.json().catch(() => ({ message: response.statusText }))
    const error = new Error(toChineseErrorMessage(payload.message || payload.error || response.statusText, '操作失败，请稍后重试')) as Error & {
      payload?: unknown
      status?: number
    }
    error.payload = payload
    error.status = response.status
    throw error
  }
  if (init.skipJson) {
    return undefined as T
  }
  return response.json() as Promise<T>
}

export const api = {
  setCSRF(value: string) {
    csrfToken = value
  },
  async getSession() {
    const session = await request<{
      user: { id: string; email: string; displayName: string } | null
      admin: { id: string; email: string; displayName: string; role: string } | null
      csrfToken: string
      userCsrfToken?: string
      adminCsrfToken?: string
      registerEnabled: boolean
      adminSetupRequired: boolean
      oauthProviders: Array<{ slug: string; name: string; logoUrl?: string }>
      authSecurity: {
        captchaRequiredOnRegister: boolean
        phoneVerificationRequiredOnRegister: boolean
        phoneLoginEnabled: boolean
        smsCodeTTLSeconds: number
        verificationTestMode?: boolean
        smsGatewayConfigured: boolean
        emailGatewayConfigured?: boolean
      }
    }>('/auth/session')
    csrfToken = resolveSessionCSRFToken(session)
    return {
      ...session,
      oauthProviders: asArray<OAuthProviderSummary>(session.oauthProviders),
      authSecurity: asObject(session.authSecurity, {
        captchaRequiredOnRegister: true,
        phoneVerificationRequiredOnRegister: true,
        phoneLoginEnabled: true,
        smsCodeTTLSeconds: 300,
        verificationTestMode: false,
        smsGatewayConfigured: false,
        emailGatewayConfigured: false,
      }),
    }
  },
  getCaptcha() {
    return request<{
      captchaId: string
      challengeType?: 'text' | 'slide' | 'choice'
      imageDataUrl: string
      prompt?: string
      options?: Array<{ label: string; value: string }>
      expiresInSeconds: number
    }>('/auth/captcha')
  },
  sendContactCode(payload: { identifier: string; purpose?: string; captchaId?: string; captchaAnswer?: string }) {
    return request<{ ok: boolean; identifier: string; kind: 'email' | 'phone'; purpose: string; expiresInSeconds: number; deliveryMode?: string; previewCode?: string }>('/auth/contact/send-code', {
      method: 'POST',
      body: JSON.stringify(payload),
    })
  },
  sendPhoneCode(payload: { phone: string; purpose?: string; captchaId: string; captchaAnswer: string }) {
    return request<{ ok: boolean; phone: string; purpose: string; expiresInSeconds: number }>('/auth/phone/send-code', {
      method: 'POST',
      body: JSON.stringify(payload),
    })
  },
  register(payload: {
    identifier?: string
    email?: string
    phone?: string
    phoneCode?: string
    verificationCode?: string
    captchaId?: string
    captchaAnswer?: string
    password: string
    displayName?: string
    affiliateCode?: string
  }) {
    return request<{ user: unknown }>('/auth/register', {
      method: 'POST',
      body: JSON.stringify(payload),
    })
  },
  login(payload: { identifier: string; password: string; captchaId: string; captchaAnswer: string }) {
    return request<{ user: unknown }>('/auth/login', {
      method: 'POST',
      body: JSON.stringify(payload),
    })
  },
  requestPasswordReset(payload: { identifier: string; captchaId: string; captchaAnswer: string }) {
    return request<{ ok: boolean; identifier: string; kind: 'email' | 'phone'; purpose: string; expiresInSeconds: number; deliveryMode?: string; previewCode?: string }>('/auth/password/forgot', {
      method: 'POST',
      body: JSON.stringify(payload),
    })
  },
  resetPassword(payload: { identifier: string; verificationCode: string; captchaId: string; captchaAnswer: string; password: string }) {
    return request<{ ok: boolean }>('/auth/password/reset', {
      method: 'POST',
      body: JSON.stringify(payload),
    })
  },
  logout() {
    return request('/auth/logout', { method: 'POST' })
  },
  adminLogin(payload: { email: string; password: string; totpCode: string }) {
    return request<{ admin: unknown }>('/auth/admin/login', {
      method: 'POST',
      body: JSON.stringify(payload),
    })
  },
  adminBootstrapStart(payload: { email: string; password: string; displayName?: string }) {
    return request<{
      setupToken: string
      email: string
      manualEntryKey: string
      provisioningUrl: string
      qrCodeDataUrl: string
      issuer: string
      totpAppHint: string
      expiresInSeconds: number
    }>('/auth/admin/bootstrap/start', {
      method: 'POST',
      body: JSON.stringify(payload),
    })
  },
  adminBootstrapComplete(payload: { setupToken: string; totpCode: string }) {
    return request<{ admin: unknown }>('/auth/admin/bootstrap/complete', {
      method: 'POST',
      body: JSON.stringify(payload),
    })
  },
  adminLogout() {
    return request('/auth/admin/logout', { method: 'POST' })
  },
  listPlans() {
    return request<{ plans: any[] }>('/billing/plans').then((response) => ({
      ...response,
      plans: asArray(response?.plans).map((plan: any) => ({
        ...plan,
        features: asArray(plan?.features),
      })),
    }))
  },
  listConversations() {
    return request<{ conversations: any[] }>('/chat/conversations').then((response) => ({
      ...response,
      conversations: asArray(response?.conversations),
    }))
  },
  listChatModels() {
    return request<{ models: ChatModelSummary[] }>('/chat/models').then((response) => ({
      ...response,
      models: asArray<ChatModelSummary>(response?.models).map((model) => ({
        ...model,
        endpoints: asArray(model?.endpoints),
      })),
    }))
  },
  createConversation(payload: { title?: string; modelSlug: string; deepSearch?: boolean }, options: RequestOptions = {}) {
    return request<any>('/chat/conversations', {
      method: 'POST',
      body: JSON.stringify(payload),
      signal: options.signal,
    })
  },
  deleteConversation(conversationId: string) {
    return request<{ ok: boolean }>(`/chat/conversations/${conversationId}`, {
      method: 'DELETE',
    })
  },
  listMessages(conversationId: string) {
    return request<{ messages: any[] }>(`/chat/conversations/${conversationId}/messages`).then((response) => ({
      ...response,
      messages: asArray(response?.messages),
    }))
  },
  getConversationShare(conversationId: string) {
    return request<{ share: any }>(`/chat/conversations/${conversationId}/share`).then((response) => ({
      ...response,
      share: response?.share && typeof response.share === 'object' ? response.share : null,
    }))
  },
  updateConversationShare(conversationId: string, payload: { enabled: boolean; requireAccessCode: boolean; accessCode?: string; collaborationEnabled: boolean }) {
    return request<{ share: any }>(`/chat/conversations/${conversationId}/share`, {
      method: 'PUT',
      body: JSON.stringify(payload),
    }).then((response) => ({
      ...response,
      share: response?.share && typeof response.share === 'object' ? response.share : null,
    }))
  },
  getPublicConversationShare(shareId: string) {
    return request<{ share: any; messages: any[] }>(`/chat/shares/${encodeURIComponent(shareId)}`).then((response) => ({
      ...response,
      share: response?.share && typeof response.share === 'object' ? response.share : null,
      messages: asArray(response?.messages),
    }))
  },
  joinSharedConversationCollaboration(shareId: string, payload: { collaborationCode: string }) {
    return request<{ ok: boolean; share?: any }>(`/chat/shares/${encodeURIComponent(shareId)}/collaboration`, {
      method: 'POST',
      body: JSON.stringify(payload),
    })
  },
  sendSharedConversationMessage(shareId: string, payload: { content: string; collaborationCode?: string }) {
    return request<{ userMessage: any; assistantMessage: any; title?: string; reasoning?: string }>(`/chat/shares/${encodeURIComponent(shareId)}/messages`, {
      method: 'POST',
      body: JSON.stringify(payload),
    })
  },
  sendMessage(conversationId: string, payload: { content: string; modelSlug: string; deepSearch?: boolean; attachmentIds?: string[]; editMessageId?: string }) {
    return request<{ userMessage: any; assistantMessage: any; title?: string }>(`/chat/conversations/${conversationId}/messages`, {
      method: 'POST',
      body: JSON.stringify(payload),
    })
  },
  sendMessageStream(
    conversationId: string,
    payload: { content: string; modelSlug: string; deepSearch?: boolean; attachmentIds?: string[]; editMessageId?: string },
    options: RequestOptions = {},
  ) {
    const headers = new Headers({
      Accept: 'text/event-stream',
      'Content-Type': 'application/json',
      'X-Infinite-API': '1',
    })
    const deviceFingerprint = resolveDeviceFingerprint()
    if (deviceFingerprint) {
      headers.set('X-Device-Fingerprint', deviceFingerprint)
    }
    if (csrfToken) {
      headers.set('X-CSRF-Token', csrfToken)
    }
    return fetch(`/chat/conversations/${conversationId}/messages?stream=1`, {
      method: 'POST',
      credentials: 'include',
      headers,
      body: JSON.stringify(payload),
      signal: options.signal,
    })
  },
  listActiveChatRuns(conversationId: string) {
    return request<{ runs: any[] }>(`/chat/conversations/${conversationId}/runs/active`).then((response) => ({
      ...response,
      runs: asArray(response?.runs),
    }))
  },
  getChatRun(runId: string) {
    return request<{ run: any }>(`/chat/runs/${encodeURIComponent(runId)}`).then((response) => ({
      ...response,
      run: response?.run && typeof response.run === 'object' ? response.run : null,
    }))
  },
  streamChatRunEvents(runId: string, afterSeq = 0, options: RequestOptions = {}) {
    const headers = new Headers({
      Accept: 'text/event-stream',
      'X-Infinite-API': '1',
    })
    const deviceFingerprint = resolveDeviceFingerprint()
    if (deviceFingerprint) {
      headers.set('X-Device-Fingerprint', deviceFingerprint)
    }
    if (csrfToken) {
      headers.set('X-CSRF-Token', csrfToken)
    }
    return fetch(`/chat/runs/${encodeURIComponent(runId)}/events?after=${encodeURIComponent(String(afterSeq))}`, {
      method: 'GET',
      credentials: 'include',
      headers,
      signal: options.signal,
    })
  },
  cancelChatRun(runId: string) {
    return request<{ ok: boolean }>(`/chat/runs/${encodeURIComponent(runId)}/cancel`, {
      method: 'POST',
      body: JSON.stringify({}),
    })
  },
  getChatArtifact(id: string) {
    return request<any>(`/chat/artifacts/${encodeURIComponent(id)}`)
  },
  saveChatArtifactVersion(id: string, files: any[]) {
    return request<any>(`/chat/artifacts/${encodeURIComponent(id)}/versions`, {
      method: 'POST',
      body: JSON.stringify({ files }),
    })
  },
  generateChatImage(payload: { conversationId: string; prompt: string; modelSlug?: string; attachmentIds?: string[]; editMessageId?: string }, options: RequestOptions = {}) {
    return request<{ userMessage: any; assistantMessage: any; generation?: any; title?: string }>('/chat/images/generations', {
      method: 'POST',
      body: JSON.stringify(payload),
      signal: options.signal,
    })
  },
  generateChatImageStream(payload: { conversationId: string; prompt: string; modelSlug?: string; attachmentIds?: string[]; editMessageId?: string }, options: RequestOptions = {}) {
    const headers = new Headers({
      Accept: 'text/event-stream',
      'Content-Type': 'application/json',
      'X-Infinite-API': '1',
    })
    const deviceFingerprint = resolveDeviceFingerprint()
    if (deviceFingerprint) {
      headers.set('X-Device-Fingerprint', deviceFingerprint)
    }
    if (csrfToken) {
      headers.set('X-CSRF-Token', csrfToken)
    }
    return fetch('/chat/images/generations?stream=1', {
      method: 'POST',
      credentials: 'include',
      headers,
      body: JSON.stringify(payload),
      signal: options.signal,
    })
  },
  initAttachmentUpload(payload: { conversationId?: string; fileName: string; mimeType: string; sizeBytes: number }) {
    return request<any>('/chat/attachments/upload-init', {
      method: 'POST',
      body: JSON.stringify(payload),
    })
  },
  uploadAttachmentBinary(uploadUrl: string, file: File) {
    const headers = new Headers({
      'Content-Type': file.type || 'application/octet-stream',
      'X-Infinite-API': '1',
    })
    const deviceFingerprint = resolveDeviceFingerprint()
    if (deviceFingerprint) {
      headers.set('X-Device-Fingerprint', deviceFingerprint)
    }
    if (csrfToken) {
      headers.set('X-CSRF-Token', csrfToken)
    }
    return fetch(uploadUrl, {
      method: 'PUT',
      credentials: 'include',
      headers,
      body: file,
    }).then(async (response) => {
      if (!response.ok) {
        const payload = await response.json().catch(() => ({ message: response.statusText }))
        throw new Error(toChineseErrorMessage(payload.message || payload.error || response.statusText, '上传失败，请稍后重试'))
      }
      return response.json().catch(() => ({ ok: true }))
    })
  },
  completeAttachment(id: string) {
    return request<any>(`/chat/attachments/${id}/complete`, {
      method: 'POST',
      body: JSON.stringify({}),
    })
  },
  listApiKeys() {
    return request<{ apiKeys: any[] }>('/developer/api-keys').then((response) => ({
      ...response,
      apiKeys: asArray(response?.apiKeys),
    }))
  },
  createApiKey(payload: { name: string; scopes: string[]; rateLimitPerMinute: number }) {
    return request<any>('/developer/api-keys', {
      method: 'POST',
      body: JSON.stringify(payload),
    })
  },
  revokeApiKey(id: string) {
    return request(`/developer/api-keys/${id}`, { method: 'DELETE' })
  },
  developerUsage() {
    return request<any>('/developer/usage')
  },
  getSubscription() {
    return request<any>('/billing/subscription')
  },
  createOrder(payload: { type: string; planCode?: string; rechargeAmount?: number; subMethod: string }) {
    return request<any>('/billing/orders', {
      method: 'POST',
      body: JSON.stringify(payload),
    })
  },
  getOrder(id: string) {
    return request<any>(`/billing/orders/${id}`)
  },
  listDownloads() {
    return request<{ releases: any[] }>('/downloads/releases').then((response) => ({
      ...response,
      releases: asArray(response?.releases),
    }))
  },
  getUserSettings() {
    return request<any>('/user/settings')
  },
  updateUserSettings(payload: { theme: string; language: string; deepSearchDefault: boolean; selectedModelSlug?: string; chatHistoryEnabled: boolean; memoryEnabled: boolean }) {
    return request('/user/settings', {
      method: 'PUT',
      body: JSON.stringify(payload),
    })
  },
  sendTemporaryMessageStream(
    payload: { history: any[]; content: string; modelSlug: string; deepSearch?: boolean; attachmentIds?: string[] },
    options: RequestOptions = {},
  ) {
    const headers = new Headers({
      Accept: 'text/event-stream',
      'Content-Type': 'application/json',
      'X-Infinite-API': '1',
    })
    const deviceFingerprint = resolveDeviceFingerprint()
    if (deviceFingerprint) {
      headers.set('X-Device-Fingerprint', deviceFingerprint)
    }
    if (csrfToken) {
      headers.set('X-CSRF-Token', csrfToken)
    }
    return fetch('/chat/temporary/messages?stream=1', {
      method: 'POST',
      credentials: 'include',
      headers,
      body: JSON.stringify(payload),
      signal: options.signal,
    })
  },
  clearChats() {
    return request('/user/chat', { method: 'DELETE' })
  },
  deleteAccount() {
    return request('/user/account', { method: 'DELETE' })
  },
  exportData() {
    const headers: Record<string, string> = {}
    const deviceFingerprint = resolveDeviceFingerprint()
    if (deviceFingerprint) {
      headers['X-Device-Fingerprint'] = deviceFingerprint
    }
    if (csrfToken) {
      headers['X-CSRF-Token'] = csrfToken
    }
    return fetch('/user/export', {
      method: 'POST',
      credentials: 'include',
      headers,
    })
  },
  redeemPreview(code: string) {
    return request<any>(`/redeem/codes/${encodeURIComponent(code)}`)
  },
  redeemClaim(code: string) {
    return request<any>('/redeem/claim', {
      method: 'POST',
      body: JSON.stringify({ code }),
    })
  },
  generateTemporaryChatImage(payload: { history: any[]; prompt: string; modelSlug?: string; attachmentIds?: string[] }, options: RequestOptions = {}) {
    return request<{ userMessage: any; assistantMessage: any; generation?: any; title?: string }>('/chat/temporary/images/generations', {
      method: 'POST',
      body: JSON.stringify(payload),
      signal: options.signal,
    })
  },
  adminDashboard() {
    return request<any>('/admin/dashboard?__api=1').then((response) =>
      asObject(response, {
        stats: [],
      }),
    )
  },
  adminUsers() {
    return request<any>('/admin/users?__api=1').then((response) => ({
      ...asObject(response, { users: [] }),
      users: asArray(response?.users),
    }))
  },
  adminUpdateUser(id: string, payload: any) {
    return request(`/admin/users/${id}`, {
      method: 'PATCH',
      body: JSON.stringify(payload),
    })
  },
  adminDeleteUser(id: string) {
    return request(`/admin/users/${id}`, {
      method: 'DELETE',
    })
  },
  adminModels() {
    return request<any>('/admin/models?__api=1').then((response) => ({
      ...asObject(response, { models: [] }),
      models: asArray(response?.models).map((model: any) => ({
        ...model,
        endpoints: asArray(model?.endpoints),
      })),
    }))
  },
  adminCreateModel(payload: any) {
    return request('/admin/models', {
      method: 'POST',
      body: JSON.stringify(payload),
    })
  },
  adminUpdateModel(slug: string, payload: any) {
    return request(`/admin/models/${slug}`, {
      method: 'PUT',
      body: JSON.stringify(payload),
    })
  },
  adminDeleteModel(slug: string) {
    return request(`/admin/models/${slug}`, {
      method: 'DELETE',
    })
  },
  adminTestModelRoute(payload: any) {
    return request<any>('/admin/models/test', {
      method: 'POST',
      body: JSON.stringify(payload),
    })
  },
  adminApiStats() {
    return request<any>('/admin/api-stats?__api=1').then((response) => ({
      ...asObject(response, { summary: { totalRequests: 0, successRate: '0.0%', avgLatencyMs: 0, errorCount: 0 }, logs: [] }),
      summary: asObject(response?.summary, { totalRequests: 0, successRate: '0.0%', avgLatencyMs: 0, errorCount: 0 }),
      logs: asArray(response?.logs),
    }))
  },
  adminSystemLogs() {
    return request<any>('/admin/system-logs?__api=1').then((response) => ({
      ...asObject(response, { logs: [] }),
      logs: asArray(response?.logs),
    }))
  },
  adminServiceAlerts() {
    return request<any>('/admin/service-alerts?__api=1').then((response) => ({
      ...asObject(response, { alerts: [] }),
      alerts: asArray(response?.alerts),
    }))
  },
  adminReadServiceAlert(id: string) {
    return request(`/admin/service-alerts/${id}/read`, {
      method: 'POST',
      body: JSON.stringify({}),
    })
  },
  adminResolveServiceAlert(id: string) {
    return request(`/admin/service-alerts/${id}/resolve`, {
      method: 'POST',
      body: JSON.stringify({}),
    })
  },
  adminMemberStats() {
    return request<any>('/admin/member-stats?__api=1').then((response) => ({
      ...asObject(response, { logs: [] }),
      logs: asArray(response?.logs),
    }))
  },
  adminMembership() {
    return request<any>('/admin/membership?__api=1').then((response) => ({
      ...asObject(response, { members: [] }),
      members: asArray(response?.members),
    }))
  },
  adminCreateRedeemCampaign(payload: any) {
    return request<any>('/admin/redeem/campaigns', {
      method: 'POST',
      body: JSON.stringify(payload),
    })
  },
  adminFinance() {
    return request<any>('/admin/finance?__api=1').then((response) => ({
      ...asObject(response, { ifpayConfig: {}, transactions: [] }),
      ifpayConfig: asObject(response?.ifpayConfig, {}),
      transactions: asArray(response?.transactions),
    }))
  },
  adminUpdateIFPay(payload: any) {
    return request('/admin/finance/ifpay', {
      method: 'PUT',
      body: JSON.stringify(payload),
    })
  },
  adminSettings() {
    return request<any>('/admin/settings?__api=1').then((response) => ({
      ...asObject(response, { registerEnabled: false, oauthProviders: [], authSecurity: {}, emailGateway: {}, smsGateway: {}, modelMembershipLimits: {}, modelContextLimits: {}, infiniteCodeQuotaConfig: {}, searchProvider: {} }),
      oauthProviders: asArray(response?.oauthProviders),
      authSecurity: asObject(response?.authSecurity, {
        captchaRequiredOnRegister: true,
        phoneVerificationRequiredOnRegister: true,
        phoneLoginEnabled: true,
        smsCodeTTLSeconds: 300,
        verificationTestMode: false,
        smsGatewayConfigured: false,
        emailGatewayConfigured: false,
      }),
      emailGateway: asObject(response?.emailGateway, {
        enabled: false,
        providerName: '',
        endpointUrl: '',
        authScheme: 'bearer',
        headerName: 'Authorization',
        authToken: '',
        fromAddress: '',
        fromName: 'Infinite-AI',
        subjectTemplate: '【Infinite-AI】您的验证码是 {{code}}',
        contentTemplate: '您的验证码是 {{code}}，{{minutes}} 分钟内有效。',
      }),
      smsGateway: asObject(response?.smsGateway, {
        enabled: false,
        providerName: '',
        endpointUrl: '',
        authScheme: 'bearer',
        headerName: 'Authorization',
        authToken: '',
        senderId: '',
        messageTemplate: '',
      }),
      modelMembershipLimits: asObject(response?.modelMembershipLimits, {}),
      modelContextLimits: asObject(response?.modelContextLimits, { default: 0, models: {}, plans: {}, users: {} }),
      infiniteCodeQuotaConfig: asObject(response?.infiniteCodeQuotaConfig, {}),
      searchProvider: asObject(response?.searchProvider, {
        enabled: true,
        provider: 'openai_then_searxng',
        baseUrl: 'http://searxng:8080',
        resultCount: 5,
        timeoutSeconds: 8,
      }),
    }))
  },
  adminUpdateRegister(enabled: boolean) {
    return request('/admin/settings/register', {
      method: 'PUT',
      body: JSON.stringify({ enabled }),
    })
  },
  adminUpdateAuthSecurity(payload: any) {
    return request('/admin/settings/auth-security', {
      method: 'PUT',
      body: JSON.stringify(payload),
    })
  },
  adminUpdateEmailGateway(payload: any) {
    return request('/admin/settings/email-gateway', {
      method: 'PUT',
      body: JSON.stringify(payload),
    })
  },
  adminTestEmailGateway(payload: { email: string }) {
    return request<any>('/admin/settings/email-gateway/test', {
      method: 'POST',
      body: JSON.stringify(payload),
    })
  },
  adminUpdateSMSGateway(payload: any) {
    return request('/admin/settings/sms-gateway', {
      method: 'PUT',
      body: JSON.stringify(payload),
    })
  },
  adminTestSMSGateway(payload: { phone: string }) {
    return request<any>('/admin/settings/sms-gateway/test', {
      method: 'POST',
      body: JSON.stringify(payload),
    })
  },
  adminUpdateModelMembershipLimits(payload: any) {
    return request('/admin/settings/model-membership-limits', {
      method: 'PUT',
      body: JSON.stringify(payload),
    })
  },
  adminUpdateModelContextLimits(payload: any) {
    return request('/admin/settings/model-context-limits', {
      method: 'PUT',
      body: JSON.stringify(payload),
    })
  },
  adminUpdateSearchProvider(payload: any) {
    return request('/admin/settings/search-provider', {
      method: 'PUT',
      body: JSON.stringify(payload),
    })
  },
  adminUpdateInfiniteCodeQuotaConfig(payload: any) {
    return request('/admin/settings/infinite-code-quota', {
      method: 'PUT',
      body: JSON.stringify(payload),
    })
  },
  adminUpdateShareCollaborationConfig(payload: any) {
    return request('/admin/settings/share-collaboration', {
      method: 'PUT',
      body: JSON.stringify(payload),
    })
  },
  adminCreateOAuth(payload: any) {
    return request('/admin/settings/oauth', {
      method: 'POST',
      body: JSON.stringify(payload),
    })
  },
  adminUpdateOAuth(slug: string, payload: any) {
    return request(`/admin/settings/oauth/${slug}`, {
      method: 'PUT',
      body: JSON.stringify(payload),
    })
  },
  adminCreateInvite() {
    return request<any>('/admin/invite-links', {
      method: 'POST',
      body: JSON.stringify({}),
    })
  },
  adminRevokeInvite(code: string) {
    return request(`/admin/invite-links/${encodeURIComponent(code)}`, {
      method: 'DELETE',
    })
  },
  adminInviteLinks() {
    return request<any>('/admin/invite-links').then((response) => ({
      ...asObject(response, { invites: [] }),
      invites: asArray(response?.invites),
    }))
  },
}
