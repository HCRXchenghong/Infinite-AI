import { useEffect, useState } from 'react'
import { Gift, ArrowRight, CheckCircle2, AlertCircle, Loader2, MailOpen, Sparkles } from 'lucide-react'
import { useNavigate } from 'react-router-dom'
import { api } from '../lib/api'
import { BRAND_LOGO_SRC } from '../lib/brand'
import { getUserAppBaseURL } from '../lib/runtime'
import { readThemePreference, type ThemePreference, useResolvedTheme } from '../lib/theme'

export function RedeemPage() {
  const navigate = useNavigate()
  const userAppBaseURL = getUserAppBaseURL()
  const [theme, setTheme] = useState<ThemePreference>(() => readThemePreference())
  const [code, setCode] = useState('')
  const [step, setStep] = useState<'input' | 'unopened' | 'opened' | 'redeeming' | 'success'>('input')
  const [error, setError] = useState('')
  const [preview, setPreview] = useState<any>(null)
  const [session, setSession] = useState<Awaited<ReturnType<typeof api.getSession>> | null>(null)

  const isDark = useResolvedTheme(theme) === 'dark'
  const colors = {
    appBg: isDark ? 'bg-[#111111]' : 'bg-[#fafafa]',
    cardBg: isDark ? 'bg-[#1a1a1a]' : 'bg-white',
    textMain: isDark ? 'text-[#ececec]' : 'text-[#111111]',
    textMuted: isDark ? 'text-[#888888]' : 'text-[#666666]',
    border: isDark ? 'border-[#333333]' : 'border-[#e5e5e5]',
    btnPrimary: isDark ? 'bg-[#ececec] text-black hover:bg-white' : 'bg-[#111111] text-white hover:bg-black',
  }

  useEffect(() => {
    void api.getSession().then((nextSession) => {
      setSession(nextSession)
      const params = new URLSearchParams(window.location.search)
      if (params.get('claimed') === '1' && nextSession.user) {
        setStep('success')
      }
    })
    const params = new URLSearchParams(window.location.search)
    const urlCode = params.get('code')
    if (urlCode) {
      setCode(urlCode)
      void verifyCode(urlCode)
    }
  }, [])

  useEffect(() => {
    const params = new URLSearchParams(window.location.search)
    if (params.get('claimed') === '1' && session?.user) {
      setStep('success')
    }
  }, [session?.user])

  async function verifyCode(inputCode: string) {
    try {
      const result = await api.redeemPreview(inputCode)
      setPreview(result)
      setStep(new URLSearchParams(window.location.search).get('claimed') === '1' ? 'success' : 'unopened')
      setError('')
    } catch (err) {
      setError(err instanceof Error ? err.message : '兑换码无效')
      setStep('input')
    }
  }

  async function handleVerifyCode(event: React.FormEvent) {
    event.preventDefault()
    if (!code.trim()) {
      setError('请输入兑换码')
      return
    }
    await verifyCode(code.trim())
  }

  async function handleRedeem() {
    if (!session?.user) {
      const target = preview?.accountType === 'no_account' ? '/register' : '/login'
      navigate(`${target}?redeem=${encodeURIComponent(code)}`, { replace: true })
      return
    }
    try {
      setStep('redeeming')
      await api.redeemClaim(code)
      setStep('success')
    } catch (err) {
      setError(err instanceof Error ? err.message : '领取失败')
      setStep('opened')
    }
  }

  return (
    <div className={`min-h-screen flex flex-col items-center justify-center transition-colors duration-500 ${colors.appBg} ${colors.textMain}`}>
      <div className="absolute top-6 right-6 z-50">
        <select value={theme} onChange={(event) => setTheme(event.target.value as ThemePreference)} className={`px-3 py-1.5 rounded-md border text-sm ${isDark ? 'bg-[#1a1a1a] border-[#333] text-white' : 'bg-white border-[#ccc] text-black'}`}>
          <option value="system">跟随系统</option>
          <option value="dark">深色主题</option>
          <option value="light">浅色主题</option>
        </select>
      </div>
      <div className="absolute top-8 flex items-center justify-center gap-2 opacity-50 z-10">
        <img src={BRAND_LOGO_SRC} alt="Infinite-AI" className="h-7 w-7 rounded-xl object-cover" />
        <span className="font-medium tracking-wide">Infinite-AI</span>
      </div>
      <div className="w-full max-w-md px-6 flex flex-col items-center justify-center min-h-[400px] relative">
        {step === 'input' && (
          <div className="w-full text-center">
            <div className="flex justify-center mb-6">
              <div className={`w-14 h-14 rounded-full flex items-center justify-center border ${isDark ? 'bg-[#1a1a1a] border-[#333]' : 'bg-white border-[#e5e5e5]'}`}>
                <Gift className={`w-6 h-6 ${colors.textMain}`} />
              </div>
            </div>
            <h1 className="text-2xl font-semibold mb-2">兑换礼品卡</h1>
            <p className={`text-sm mb-8 ${colors.textMuted}`}>请输入您的专属兑换码以解锁 Infinite-AI 权益</p>
            <form onSubmit={handleVerifyCode} className="space-y-4">
              <input value={code} onChange={(event) => setCode(event.target.value)} placeholder="输入兑换码 (如 GIFT-...)" className={`w-full px-5 py-4 rounded-xl border text-center font-mono text-base ${isDark ? 'bg-[#1a1a1a] border-[#333]' : 'bg-white border-gray-200 shadow-sm'} ${colors.textMain}`} />
              {error && (
                <div className="flex items-center justify-center gap-1.5 text-red-500 text-sm">
                  <AlertCircle className="w-4 h-4" />
                  <span>{error}</span>
                </div>
              )}
              <button type="submit" disabled={!code.trim()} className={`w-full py-4 rounded-xl text-base font-medium ${!code.trim() ? 'bg-gray-500/20 text-gray-500 cursor-not-allowed' : colors.btnPrimary}`}>继续</button>
            </form>
          </div>
        )}

        {step === 'unopened' && (
          <div onClick={() => setStep('opened')} className="flex flex-col items-center cursor-pointer group py-8">
            <div className="relative w-40 h-40 flex items-center justify-center mb-6">
              <div className={`absolute inset-0 rounded-full blur-3xl ${isDark ? 'bg-gradient-to-r from-blue-500/20 to-purple-500/20' : 'bg-gradient-to-r from-blue-500/10 to-purple-500/10'}`} />
              <Gift className="w-24 h-24 text-blue-500 relative z-10 group-hover:-translate-y-3 transition-transform duration-500" />
              <Sparkles className="w-6 h-6 text-purple-400 absolute top-4 right-4 opacity-0 group-hover:opacity-100 transition-all duration-700" />
            </div>
            <h2 className={`text-xl font-medium tracking-wide mb-4 group-hover:text-blue-500 ${colors.textMain}`}>您有一份专属礼遇待查收</h2>
            <div className={`text-sm px-6 py-2 rounded-full border ${colors.border} ${colors.textMuted}`}>点击拆开</div>
          </div>
        )}

        {(step === 'opened' || step === 'redeeming') && (
          <div className="w-full flex flex-col items-center relative z-10">
            <div className={`w-full ${colors.cardBg} border ${colors.border} rounded-xl shadow-xl overflow-hidden p-8`}>
              <div className="flex flex-col items-center text-center">
                <MailOpen className={`w-8 h-8 mb-6 ${colors.textMain}`} />
                <h3 className={`text-2xl font-semibold mb-2 ${colors.textMain}`}>Infinite 权益卡</h3>
                <p className={`text-sm mb-8 ${colors.textMuted}`}>感谢您的支持，以下是为您准备的订阅权益。</p>
                <div className={`w-full text-left p-5 rounded-lg border ${isDark ? 'bg-[#111111] border-[#333]' : 'bg-[#f9f9f9] border-[#e5e5e5]'} space-y-4`}>
                  <div className="flex justify-between items-center">
                    <span className={`text-sm ${colors.textMuted}`}>包含套餐</span>
                    <span className={`text-base font-semibold ${colors.textMain}`}>{preview?.planName ?? '-'}</span>
                  </div>
                  <div className={`h-px w-full ${colors.border} border-t`} />
                  <div className="flex justify-between items-center">
                    <span className={`text-sm ${colors.textMuted}`}>有效时长</span>
                    <span className={`text-base font-medium ${colors.textMain}`}>{preview?.durationText ?? '-'}</span>
                  </div>
                </div>
              </div>
            </div>
            <div className="w-full mt-6">
              <button onClick={() => void handleRedeem()} disabled={step === 'redeeming'} className={`w-full py-4 rounded-xl text-base font-medium flex items-center justify-center gap-2 ${step === 'redeeming' ? 'bg-gray-500/20 text-gray-500 cursor-not-allowed' : colors.btnPrimary}`}>
                {step === 'redeeming' ? (
                  <>
                    <Loader2 className="w-5 h-5 animate-spin" />
                    正在激活...
                  </>
                ) : (
                  <>
                    {session?.user ? '确认领取' : preview?.accountType === 'no_account' ? '注册后领取' : '登录后领取'} <ArrowRight className="w-5 h-5" />
                  </>
                )}
              </button>
              <p className={`text-center text-xs mt-4 ${colors.textMuted}`}>
                {session?.user ? '领取后将自动绑定至您当前登录的账户' : preview?.accountType === 'no_account' ? '继续后会进入注册流程，并在注册成功后自动绑定此权益。' : '继续后会进入登录流程，并在登录成功后自动绑定此权益。'}
              </p>
              {error && <p className="text-center text-xs mt-3 text-red-500">{error}</p>}
            </div>
          </div>
        )}

        {step === 'success' && (
          <div className="text-center w-full pt-8">
            <CheckCircle2 className="w-10 h-10 text-green-500 mx-auto mb-4" />
            <h2 className={`text-3xl font-semibold mb-4 ${colors.textMain}`}>激活成功</h2>
            <p className={`text-sm mb-12 ${colors.textMuted}`}>权益已绑定至 <span className={`font-medium ${colors.textMain}`}>{session?.user?.email}</span></p>
            <a href={userAppBaseURL} className={`w-full inline-flex justify-center py-4 rounded-xl text-base font-medium transition-all ${colors.btnPrimary}`}>进入工作台</a>
          </div>
        )}
      </div>
    </div>
  )
}
