import { useEffect, useState } from 'react'
import { ShieldCheck, QrCode, KeyRound } from 'lucide-react'
import { useNavigate } from 'react-router-dom'
import { api } from '../lib/api'
import { BRAND_LOGO_SRC } from '../lib/brand'
import { readThemePreference, type ThemePreference, useResolvedTheme } from '../lib/theme'

type LoginFormState = {
  email: string
  password: string
  totpCode: string
}

type SetupState = {
  setupToken: string
  email: string
  manualEntryKey: string
  provisioningUrl: string
  qrCodeDataUrl: string
  issuer: string
  totpAppHint: string
  expiresInSeconds: number
}

export function AdminLoginPage() {
  const navigate = useNavigate()
  const [loading, setLoading] = useState(true)
  const [adminSetupRequired, setAdminSetupRequired] = useState(false)
  const [setupState, setSetupState] = useState<SetupState | null>(null)
  const [form, setForm] = useState<LoginFormState>({ email: '', password: '', totpCode: '' })
  const [error, setError] = useState('')
  const [theme, setTheme] = useState<ThemePreference>(() => readThemePreference())
  const isDark = useResolvedTheme(theme) === 'dark'
  const colors = {
    appBg: isDark ? 'bg-[#111111]' : 'bg-[#fafafa]',
    panelBg: isDark ? 'bg-[#171717]' : 'bg-white',
    textMain: isDark ? 'text-white' : 'text-[#111111]',
    textMuted: isDark ? 'text-gray-400' : 'text-[#666666]',
    border: isDark ? 'border-[#333]' : 'border-[#e5e5e5]',
    input: isDark ? 'border-[#333] bg-[#111] text-white' : 'border-[#d8d8d8] bg-white text-[#111111]',
    primary: isDark ? 'bg-white text-black hover:bg-gray-200' : 'bg-[#111111] text-white hover:bg-black',
  }

  useEffect(() => {
    document.title = adminSetupRequired ? 'Infinite-AI 初始化后台' : 'Infinite-AI 后台登录'
  }, [adminSetupRequired])

  useEffect(() => {
    void (async () => {
      try {
        const session = await api.getSession()
        if (session.admin) {
          navigate('/admin/dashboard', { replace: true })
          return
        }
        setAdminSetupRequired(Boolean(session.adminSetupRequired))
      } finally {
        setLoading(false)
      }
    })()
  }, [navigate])

  async function handleLogin(event: React.FormEvent) {
    event.preventDefault()
    setError('')
    try {
      await api.adminLogin({
        email: form.email,
        password: form.password,
        totpCode: form.totpCode,
      })
      navigate('/admin/dashboard', { replace: true })
    } catch (err) {
      setError(err instanceof Error ? err.message : '登录失败')
    }
  }

  async function handleBootstrapStart(event: React.FormEvent) {
    event.preventDefault()
    setError('')
    try {
      const response = await api.adminBootstrapStart({
        email: form.email,
        password: form.password,
      })
      setSetupState(response)
      setForm((prev) => ({ ...prev, totpCode: '' }))
    } catch (err) {
      setError(err instanceof Error ? err.message : '初始化失败')
    }
  }

  async function handleBootstrapComplete(event: React.FormEvent) {
    event.preventDefault()
    if (!setupState) return
    setError('')
    try {
      await api.adminBootstrapComplete({
        setupToken: setupState.setupToken,
        totpCode: form.totpCode,
      })
      navigate('/admin/dashboard', { replace: true })
    } catch (err) {
      setError(err instanceof Error ? err.message : '验证失败')
    }
  }

  if (loading) {
    return <div className={`min-h-screen flex items-center justify-center ${colors.appBg} ${colors.textMain}`}>正在加载...</div>
  }

  return (
    <div className={`min-h-screen flex items-center justify-center p-6 ${colors.appBg} ${colors.textMain}`}>
      <div className="absolute top-6 right-6 z-50">
        <select value={theme} onChange={(event) => setTheme(event.target.value as ThemePreference)} className={`px-3 py-1.5 rounded-md border text-sm ${colors.input}`}>
          <option value="system">跟随系统</option>
          <option value="dark">深色主题</option>
          <option value="light">浅色主题</option>
        </select>
      </div>
      <div className={`w-full max-w-md rounded-2xl border p-8 shadow-2xl ${colors.border} ${colors.panelBg}`}>
        <div className="flex items-center justify-center mb-6">
          <img src={BRAND_LOGO_SRC} alt="Infinite-AI" className="h-12 w-12 rounded-2xl object-cover" />
        </div>
        <h1 className="text-2xl font-semibold text-center mb-2">Infinite-AI 管理后台</h1>
        <p className={`text-sm text-center mb-8 ${colors.textMuted}`}>
          {adminSetupRequired ? '首次进入后台，请先创建超级管理员账号并绑定 2FA。' : '管理员登录需要邮箱、密码和 2FA 验证码。'}
        </p>

        {adminSetupRequired && !setupState && (
          <form onSubmit={handleBootstrapStart} className="space-y-4">
            <div className="rounded-xl border border-[#2d3b52] bg-[#111827] px-4 py-3 text-sm text-blue-200">
              <div className="flex items-center gap-2 font-medium">
                <ShieldCheck className="w-4 h-4" />
                首次初始化
              </div>
              <p className="mt-2 text-xs text-blue-200/80">创建完成后，这个入口会自动切换成常规登录页，不再允许再次首登注册。</p>
            </div>
            <input
              value={form.email}
              onChange={(event) => setForm((prev) => ({ ...prev, email: event.target.value }))}
              className={`w-full rounded-lg border px-4 py-3 ${colors.input}`}
              placeholder="管理员邮箱"
              type="email"
              required
            />
            <input
              value={form.password}
              onChange={(event) => setForm((prev) => ({ ...prev, password: event.target.value }))}
              className={`w-full rounded-lg border px-4 py-3 ${colors.input}`}
              type="password"
              placeholder="管理员密码"
              required
            />
            {error && <div className="text-sm text-red-500">{error}</div>}
            <button className={`w-full rounded-lg py-3 text-sm font-semibold ${colors.primary}`}>创建管理员并继续绑定 2FA</button>
          </form>
        )}

        {adminSetupRequired && setupState && (
          <form onSubmit={handleBootstrapComplete} className="space-y-5">
            <div className={`rounded-2xl border p-4 ${colors.border} ${isDark ? 'bg-[#111]' : 'bg-[#f8f8f8]'}`}>
              <div className="flex items-center gap-2 text-sm font-medium">
                <QrCode className="w-4 h-4" />
                使用 {setupState.totpAppHint} 扫码绑定
              </div>
              <img src={setupState.qrCodeDataUrl} alt="2FA QR Code" className="mx-auto my-4 h-52 w-52 rounded-xl bg-white p-3" />
              <div className={`rounded-lg border px-3 py-3 text-xs ${colors.border} ${isDark ? 'bg-[#0b0b0b] text-gray-300' : 'bg-white text-[#555]'}`}>
                <div className="flex items-center gap-2 font-medium">
                  <KeyRound className="w-3.5 h-3.5" />
                  无法扫码时手动输入
                </div>
                <div className="mt-2 break-all font-mono tracking-[0.2em] text-green-400">{setupState.manualEntryKey}</div>
              </div>
            </div>
            <input
              value={form.totpCode}
              onChange={(event) => setForm((prev) => ({ ...prev, totpCode: event.target.value }))}
              className={`w-full rounded-lg border px-4 py-3 ${colors.input}`}
              placeholder="输入 Microsoft Authenticator 当前 6 位验证码"
              inputMode="numeric"
              required
            />
            {error && <div className="text-sm text-red-500">{error}</div>}
            <button className={`w-full rounded-lg py-3 text-sm font-semibold ${colors.primary}`}>完成初始化并登录后台</button>
          </form>
        )}

        {!adminSetupRequired && (
          <form onSubmit={handleLogin} className="space-y-4">
            <input
              value={form.email}
              onChange={(event) => setForm((prev) => ({ ...prev, email: event.target.value }))}
              className={`w-full rounded-lg border px-4 py-3 ${colors.input}`}
              placeholder="邮箱"
              type="email"
              required
            />
            <input
              value={form.password}
              onChange={(event) => setForm((prev) => ({ ...prev, password: event.target.value }))}
              className={`w-full rounded-lg border px-4 py-3 ${colors.input}`}
              type="password"
              placeholder="密码"
              required
            />
            <input
              value={form.totpCode}
              onChange={(event) => setForm((prev) => ({ ...prev, totpCode: event.target.value }))}
              className={`w-full rounded-lg border px-4 py-3 ${colors.input}`}
              placeholder="2FA 验证码"
              inputMode="numeric"
              required
            />
            {error && <div className="text-sm text-red-500">{error}</div>}
            <button className={`w-full rounded-lg py-3 text-sm font-semibold ${colors.primary}`}>登录后台</button>
          </form>
        )}
      </div>
    </div>
  )
}
