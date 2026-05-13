import { useEffect, useMemo, useRef, useState } from 'react'
import type { ClipboardEvent, ComponentType, DragEvent, ReactNode } from 'react'
import { flushSync } from 'react-dom'
import Editor from '@monaco-editor/react'
import {
  Menu,
  Settings,
  LogOut,
  Zap,
  Paperclip,
  Image as ImageIcon,
  Globe,
  ChevronDown,
  Check,
  X,
  ArrowUp,
  PanelLeftClose,
  PanelLeft,
  SquarePen,
  Download,
  Code2,
  Copy,
  CheckCircle2,
  Trash2,
  Smartphone,
  Monitor,
  BookOpen,
  Terminal,
  ArrowLeft,
  ShieldCheck,
  Coins,
  Lock,
  Share2,
  Maximize2,
  Users,
} from 'lucide-react'
import { useLocation, useNavigate } from 'react-router-dom'
import { api } from '../lib/api'
import { BRAND_LOGO_SRC } from '../lib/brand'
import { getPublicAPIBaseURL } from '../lib/runtime'
import { normalizeThemePreference, readThemePreference, type ThemePreference, useResolvedTheme } from '../lib/theme'

type SessionState = Awaited<ReturnType<typeof api.getSession>>

type OAuthProviderOption = {
  slug: string
  name: string
  logoUrl?: string
}

type ModelOption = {
  slug: string
  label?: string
  name?: string
  desc?: string
  description?: string
}

type ComposerAttachment = {
  clientId: string
  id?: string
  fileName: string
  mimeType: string
  sizeBytes?: number
  previewUrl?: string
  status: 'uploading' | 'ready' | 'error'
  error?: string
}

type AuthFormState = {
  identifier: string
  verificationCode: string
  captchaAnswer: string
  password: string
  confirmPassword: string
  displayName: string
}

type RegisterStep = 'identity' | 'verify' | 'password' | 'captcha' | 'success'
type PasswordResetStep = 'identity' | 'password' | 'success'

type RegisterVerificationState = {
  masked: string
  kind: 'email' | 'phone'
  previewCode?: string
  deliveryMode?: string
} | null

type CaptchaState = {
  captchaId: string
  challengeType?: 'text' | 'slide' | 'choice'
  imageDataUrl: string
  prompt?: string
  options?: Array<{ label: string; value: string }>
  expiresInSeconds: number
}

type StreamingAssistantField = 'content' | 'reasoningContent'

type MessageContentSegment =
  | { type: 'text'; text: string }
  | { type: 'code'; language: string; code: string }

type ActiveUserRequest = {
  id: number
  controller: AbortController
  conversationId: string | null
  runId?: string
  lastSeq?: number
  isDeepSearch: boolean
  phase: 'thinking' | 'answering'
}

type MessageUpdater = any[] | ((previous: any[]) => any[])

type ArtifactFile = {
  path: string
  language?: string
  content: string
}

function createEmptyAuthForm(): AuthFormState {
  return {
    identifier: '',
    verificationCode: '',
    captchaAnswer: '',
    password: '',
    confirmPassword: '',
    displayName: '',
  }
}

const FALLBACK_MODEL_OPTIONS: ModelOption[] = [
  { slug: 'infinite-ai-standard', label: 'Infinite-AI Standard', desc: '日常任务的极速模型' },
  { slug: 'infinite-ai-pro', label: 'Infinite-AI Pro', desc: '强大的推理与创作模型' },
]

const OPENAI_DOCS_URL = 'https://platform.openai.com/docs/overview'
const OPENAI_API_REFERENCE_URL = 'https://platform.openai.com/docs/api-reference/introduction'
const ANTHROPIC_DOCS_URL = 'https://docs.anthropic.com/en/docs/overview'
const ANTHROPIC_API_REFERENCE_URL = 'https://docs.anthropic.com/en/api/messages'

const SELECTED_MODEL_STORAGE_KEY = 'infinite-ai:selected-model'
const DEEP_SEARCH_STORAGE_KEY = 'infinite-ai:deep-search'

type ModelLimitState = {
  reason: string
  modelSlug: string
  modelName: string
  planCode: string
  limit: number
  used: number
  windowHours: number
}

function isAssistantRole(role: string | null | undefined) {
  const normalized = String(role ?? '').toLowerCase()
  return normalized === 'assistant' || normalized === 'ai'
}

function normalizeProviderRoleForClient(role: string | null | undefined) {
  return isAssistantRole(role) ? 'assistant' : String(role ?? 'user').toLowerCase()
}

function toUserFacingChatErrorMessage(value?: unknown, fallback = '与服务器断联，请重试') {
  const text = String(value ?? '').trim()
  if (!text) return fallback
  const lower = text.toLowerCase()
  if (lower.includes('not allowed') || lower.includes('forbidden')) return '当前账号无权执行该操作'
  if (lower.includes('captcha answer is incorrect') || lower.includes('captcha')) return '图形验证码不正确'
  if (lower.includes('email or phone') && lower.includes('password')) return '邮箱、手机号或密码不正确'
  if (lower.includes('context deadline exceeded') || lower.includes('deadline') || lower.includes('timeout') || lower.includes('network') || lower.includes('failed to fetch')) return '与服务器断联，请重试'
  if (lower === 'not found' || lower.includes('not found')) return '内容不存在或已被删除'
  if (lower.includes('rate limit') || lower.includes('too many requests')) return '请求过于频繁，请稍后再试'
  if (/^[A-Za-z0-9_ .:/?&=-]+$/.test(text) && !/[一-龥]/.test(text)) return fallback
  return text
}

function isAbortError(error: unknown) {
  return error instanceof Error && error.name === 'AbortError'
}

function isNotFoundRequestError(error: unknown) {
  if (!error || typeof error !== 'object') {
    return false
  }
  const status = Number((error as { status?: number }).status ?? 0)
  if (status === 404) {
    return true
  }
  const message = String((error as { message?: string }).message ?? '').toLowerCase()
  return message.includes('not found') || message.includes('不存在') || message.includes('已被删除')
}

function buildChatRoute(conversationId?: string | null) {
  if (!conversationId) {
    return '/?new=1'
  }
  return `/?c=${encodeURIComponent(conversationId)}`
}

function createOptimisticUserMessage(content: string, modelSlug: string, attachments: any[] = []) {
  return {
    id: `local-user-${Date.now()}-${Math.random().toString(36).slice(2, 8)}`,
    role: 'user',
    content,
    attachments,
    modelSlug,
    createdAt: new Date().toISOString(),
    optimistic: true,
  }
}

function createLocalAssistantNoticeMessage(content: string, modelSlug: string) {
  return {
    id: `local-assistant-${Date.now()}-${Math.random().toString(36).slice(2, 8)}`,
    role: 'assistant',
    content,
    reasoningContent: '',
    attachments: [],
    modelSlug,
    createdAt: new Date().toISOString(),
    optimistic: true,
  }
}

function createOptimisticImagePlaceholderMessage(modelSlug: string, id?: string, optimistic = true, label = '正在生成照片') {
  return {
    id: id || `local-image-${Date.now()}-${Math.random().toString(36).slice(2, 8)}`,
    role: 'assistant',
    content: '',
    reasoningContent: '',
    modelSlug,
    createdAt: new Date().toISOString(),
    optimistic,
    attachments: [
      {
        id: `pending-image-${Date.now()}-${Math.random().toString(36).slice(2, 8)}`,
        fileName: label,
        mimeType: 'image/png',
        pending: true,
      },
    ],
  }
}

function isPendingImagePlaceholder(message: any) {
  return Boolean(
    message &&
      isAssistantRole(message.role) &&
      Array.isArray(message.attachments) &&
      message.attachments.some((asset: any) => asset?.pending),
  )
}

function isImageAssistantMessage(message: any) {
  return Boolean(
    message &&
      isAssistantRole(message.role) &&
      Array.isArray(message.attachments) &&
      message.attachments.some((asset: any) => String(asset?.mimeType ?? '').startsWith('image/')),
  )
}

function isVisibleAssistantProgressMessage(message: any) {
  if (!message || !isAssistantRole(message.role)) {
    return false
  }
  if (isPendingImagePlaceholder(message)) {
    return true
  }
  if (String(message.id ?? '').startsWith('stream-')) {
    return true
  }
  if (message.optimistic) {
    return true
  }
  return false
}

function mapMessageAttachmentsToComposer(message: any): ComposerAttachment[] {
  if (!Array.isArray(message?.attachments)) {
    return []
  }
  return message.attachments.map((asset: any) => ({
    clientId: asset.id ?? `${message.id}-${asset.fileName ?? 'attachment'}`,
    id: asset.id,
    fileName: asset.fileName ?? '附件',
    mimeType: asset.mimeType ?? 'application/octet-stream',
    previewUrl: String(asset.mimeType ?? '').startsWith('image/') && asset.id ? `/chat/assets/${asset.id}` : undefined,
    status: 'ready' as const,
  }))
}

function trimMessagesAfter(items: any[], messageId: string) {
  const targetIndex = items.findIndex((item) => item.id === messageId)
  if (targetIndex < 0) {
    return items
  }
  return items.slice(0, targetIndex + 1)
}

function isGenericConversationTitle(title: string | null | undefined) {
  const normalized = String(title ?? '').trim()
  return !normalized || ['新聊天', '新对话', '临时聊天', 'Untitled chat'].includes(normalized)
}

function stripConversationPrefix(title: string) {
  const prefixes = ['请帮我', '帮我', '给我', '麻烦你', '请你', '请', '我想让你', '我想请你', '我想', '可以帮我', '能不能帮我']
  for (const prefix of prefixes) {
    if (title.startsWith(prefix) && [...title].length > [...prefix].length + 3) {
      return title.slice(prefix.length).trim()
    }
  }
  return title
}

function stripConversationActionPrefix(title: string) {
  const prefixes = ['系统梳理一下', '梳理一下', '梳理下', '整理', '整理一下', '整理下', '总结一下', '总结下', '概括一下', '概括下', '分析一下', '分析下', '解释一下', '解释下', '介绍一下', '介绍下', '说一下', '说下', '看一下', '看下', '写一个', '写一下', '写下', '做一个', '生成一个']
  for (const prefix of prefixes) {
    if (title.startsWith(prefix) && [...title].length > [...prefix].length + 4) {
      return title.slice(prefix.length).trim()
    }
  }
  return title
}

function firstMeaningfulConversationLine(title: string) {
  const match = title.match(/[\n\r。！？!?；;，,：:]/)
  if (!match || match.index === undefined || match.index <= 0) {
    return title
  }
  const candidate = title.slice(0, match.index).trim()
  return [...candidate].length >= 5 ? candidate : title
}

function deriveConversationTitle(content: string) {
  let title = content.trim()
  if (!title) {
    return '新聊天'
  }
  title = title.replace(/\u00a0/g, ' ')
  title = title.replace(/\s+/g, ' ').trim()
  title = title.replace(/^[\s\-•*#>"'“”‘’`()\u005b\u005d{}]+|[\s\-•*#>"'“”‘’`()\u005b\u005d{}]+$/g, '')
  title = stripConversationPrefix(title)
  title = stripConversationActionPrefix(title)
  title = firstMeaningfulConversationLine(title)
  title = title.replace(/^[\s\-•*#>"'“”‘’`()\u005b\u005d{}。，！？!?；;：:]+|[\s\-•*#>"'“”‘’`()\u005b\u005d{}。，！？!?；;：:]+$/g, '')
  if (!title) {
    return '新聊天'
  }
  const runes = [...title]
  if (runes.length > 26) {
    title = runes.slice(0, 26).join('').trim().replace(/[，,、:：;；\s]+$/g, '')
  }
  return title || '新聊天'
}

function getModelLabel(model: ModelOption | null | undefined) {
  return model?.label ?? model?.name ?? '选择模型'
}

function isImageGenerationIntent(value: string, hasImageAttachment = false) {
  const text = value.trim()
  if (!text) {
    return false
  }
  if (/^\/(?:image|img)\b/i.test(text)) {
    return true
  }
  const imageSubjectPattern = /(图片|照片|图像|头像|海报|壁纸|插画|封面|肖像|肖像照|人像|写真|证件照|图标|logo|摄影|影棚|构图|光效)/i
  return (
    /(帮我|给我|请).{0,8}(生成|画|绘制|做|创建).{0,24}(图片|照片|图像|头像|海报|壁纸|插画|封面|肖像|肖像照|人像|写真|证件照|图标|logo)/i.test(text) ||
    /(生成|画|绘制|做|创建).{0,12}(图片|照片|图像|头像|海报|壁纸|插画|封面|肖像|肖像照|人像|写真|证件照|图标|logo)/i.test(text) ||
    (/(生成|画|绘制|做|创建|出图|成图).{0,12}(一张|1张|一幅|一个|一版)/.test(text) && imageSubjectPattern.test(text)) ||
    (hasImageAttachment && /(直接)?(生成|画|绘制|做|创建|出图|成图|改成|换成|修成).{0,18}(一张|1张|一幅|一个|一版|图片|照片|图像|头像|肖像|人像|写真)?/.test(text)) ||
    /\b(generate|create|draw|make)\b[\s\S]{0,24}\b(image|picture|photo|poster|illustration|wallpaper|avatar)\b/i.test(text)
  )
}

function getPlanLabel(planCode: string) {
  switch (planCode) {
    case 'go':
      return 'Go 版'
    case 'plus':
      return 'Plus 版'
    case 'pro_basic':
      return 'Pro 基础版'
    case 'pro_max':
      return 'Pro 满血版'
    default:
      return '免费版'
  }
}

function readStoredSelectedModelSlug() {
  try {
    return window.localStorage.getItem(SELECTED_MODEL_STORAGE_KEY) ?? ''
  } catch {
    return ''
  }
}

function storeSelectedModelSlug(slug: string) {
  try {
    if (slug) {
      window.localStorage.setItem(SELECTED_MODEL_STORAGE_KEY, slug)
      return
    }
    window.localStorage.removeItem(SELECTED_MODEL_STORAGE_KEY)
  } catch {
    // Local storage can be unavailable in private/restricted browsing modes.
  }
}

function readStoredDeepSearchPreference() {
  try {
    const raw = window.localStorage.getItem(DEEP_SEARCH_STORAGE_KEY)
    if (raw === '1') {
      return true
    }
    if (raw === '0') {
      return false
    }
    return null
  } catch {
    return null
  }
}

function storeDeepSearchPreference(value: boolean) {
  try {
    window.localStorage.setItem(DEEP_SEARCH_STORAGE_KEY, value ? '1' : '0')
  } catch {
    // Preference persistence is best-effort only.
  }
}

function normalizeModelLimitState(payload: any): ModelLimitState | null {
  const source = payload?.limit
  if (!source || typeof source !== 'object') {
    return null
  }
  return {
    reason: String(source.reason ?? ''),
    modelSlug: String(source.modelSlug ?? ''),
    modelName: String(source.modelName ?? source.modelSlug ?? ''),
    planCode: String(source.planCode ?? 'free'),
    limit: Number(source.limit ?? 0),
    used: Number(source.used ?? 0),
    windowHours: Number(source.windowHours ?? 24),
  }
}

function buildTemporaryHistoryPayload(items: any[]) {
  return items
    .filter((item) => {
      if (!item || typeof item !== 'object') {
        return false
      }
      const content = typeof item.content === 'string' ? item.content.trim() : ''
      const attachments = Array.isArray(item.attachments) ? item.attachments : []
      return Boolean(content || attachments.length > 0)
    })
    .map((item) => ({
      id: item.id,
      role: item.role,
      content: item.content ?? '',
      attachments: Array.isArray(item.attachments) ? item.attachments : [],
      modelSlug: item.modelSlug ?? '',
    }))
}

function mapComposerAttachmentsToMessageAssets(items: ComposerAttachment[]) {
  return items
    .filter((item) => item.status === 'ready' && item.id)
    .map((item) => ({
      id: item.id,
      fileName: item.fileName,
      mimeType: item.mimeType,
      url: item.previewUrl,
    }))
}

function isImageMimeType(mimeType: string) {
  return String(mimeType ?? '').startsWith('image/')
}

function formatFileSize(sizeBytes?: number) {
  if (!sizeBytes || sizeBytes <= 0) {
    return ''
  }
  if (sizeBytes < 1024) {
    return `${sizeBytes} B`
  }
  if (sizeBytes < 1024 * 1024) {
    return `${(sizeBytes / 1024).toFixed(1)} KB`
  }
  return `${(sizeBytes / (1024 * 1024)).toFixed(1)} MB`
}

function buildConversationShareClipboardText(shareURL: string) {
  return String(shareURL ?? '').trim()
}

function buildConversationCollaborationClipboardText(shareURL: string, collaborationCode: string) {
  const normalizedURL = String(shareURL ?? '').trim()
  const normalizedCode = String(collaborationCode ?? '').trim()
  return normalizedCode ? `${normalizedURL}\n协作码：${normalizedCode}` : normalizedURL
}

function buildMessageAnchorShareURL(baseURL: string, messageId: string) {
  const normalizedURL = String(baseURL ?? '').trim()
  const normalizedMessageId = String(messageId ?? '').trim()
  if (!normalizedURL || !normalizedMessageId) {
    return normalizedURL
  }
  const [withoutHash] = normalizedURL.split('#')
  return `${withoutHash}#message-${encodeURIComponent(normalizedMessageId)}`
}

function normalizeVisibleMessageContent(value: unknown) {
  if (value === null || value === undefined) {
    return ''
  }
  const text = String(value)
  if (text.trim().toLowerCase() === 'null') {
    return ''
  }
  return text
    .replace(/^\s*\[Attachment:\s+[^\]]+\]\s*$/gim, '')
    .replace(/\n{3,}/g, '\n\n')
    .trimEnd()
}

function splitMessageContent(text: string): MessageContentSegment[] {
  const source = normalizeVisibleMessageContent(text)
  if (!source) {
    return []
  }
  const segments: MessageContentSegment[] = []
  const codeFence = /```([^\n`]*)\n([\s\S]*?)```/g
  let cursor = 0
  let match: RegExpExecArray | null
  while ((match = codeFence.exec(source))) {
    if (match.index > cursor) {
      segments.push({ type: 'text', text: source.slice(cursor, match.index) })
    }
    const language = String(match[1] ?? '').trim().split(/\s+/)[0]?.toLowerCase() ?? ''
    segments.push({ type: 'code', language, code: String(match[2] ?? '').replace(/\n$/, '') })
    cursor = match.index + match[0].length
  }
  if (cursor < source.length) {
    segments.push({ type: 'text', text: source.slice(cursor) })
  }
  return segments
}

function isPreviewableCode(language: string, code: string) {
  const normalized = language.toLowerCase()
  if (['html', 'htm', 'svg', 'jsx', 'tsx', 'react', 'vue'].includes(normalized)) {
    return true
  }
  return /^\s*(<!doctype html|<html[\s>]|<svg[\s>])/i.test(code) || /ReactDOM|createRoot\(|createApp\(|<template>/i.test(code)
}

function buildCodePreviewDocument(language: string, code: string) {
  if (language.toLowerCase() === 'svg' || /^\s*<svg[\s>]/i.test(code)) {
    return `<!doctype html><html><head><meta charset="utf-8"><style>html,body{margin:0;min-height:100%;display:grid;place-items:center;background:#111;color:#eee;font-family:sans-serif;padding:24px;box-sizing:border-box}svg{max-width:100%;height:auto}</style></head><body>${code}</body></html>`
  }
  if (/^\s*(<!doctype html|<html[\s>])/i.test(code)) {
    return code
  }
  return `<!doctype html><html><head><meta charset="utf-8"><style>body{margin:0;background:#fff;color:#111;font-family:ui-sans-serif,system-ui;padding:24px}</style></head><body>${code}</body></html>`
}

function buildArtifactPreviewDocument(files: ArtifactFile[], entryFile = '') {
  const normalizedFiles = Array.isArray(files) ? files : []
  const entry = normalizedFiles.find((file) => file.path === entryFile) ?? normalizedFiles[0]
  if (!entry) {
    return '<!doctype html><html><body style="font-family:sans-serif;padding:24px">暂无可预览文件</body></html>'
  }
  const kind = inferArtifactKind(normalizedFiles, entry)
  if (kind === 'react') {
    const appFile = normalizedFiles.find((file) => /App\.(jsx|tsx|js|ts)$/i.test(file.path)) ?? entry
    return buildReactPreviewDocument(appFile.content)
  }
  if (kind === 'vue') {
    const appFile = normalizedFiles.find((file) => /\.vue$/i.test(file.path)) ?? entry
    return buildVuePreviewDocument(appFile.content)
  }
  return buildCodePreviewDocument(entry.language ?? '', entry.content)
}

function inferArtifactKind(files: ArtifactFile[], entry: ArtifactFile) {
  if (files.some((file) => /App\.(jsx|tsx)$/i.test(file.path))) return 'react'
  if (files.some((file) => /\.vue$/i.test(file.path))) return 'vue'
  const language = String(entry.language ?? '').toLowerCase()
  if (['jsx', 'tsx', 'react'].includes(language)) return 'react'
  if (language === 'vue') return 'vue'
  return language || 'html'
}

function escapeScriptForHTML(value: string) {
  return String(value ?? '').replace(/<\/script/gi, '<\\/script')
}

function buildReactPreviewDocument(source: string) {
  let code = String(source ?? '')
  code = code.replace(/^\s*import\s+.*?from\s+['"][^'"]+['"];?\s*$/gm, '')
  code = code.replace(/^\s*import\s+['"][^'"]+['"];?\s*$/gm, '')
  code = code.replace(/export\s+default\s+function\s+([A-Za-z0-9_]+)/, 'function $1')
  code = code.replace(/export\s+default\s+/, 'const App = ')
  if (!/createRoot\s*\(/.test(code)) {
    code += `\n\nconst InfiniteAIApp = typeof App !== 'undefined' ? App : (typeof Demo !== 'undefined' ? Demo : null);\nif (InfiniteAIApp) ReactDOM.createRoot(document.getElementById('root')).render(<InfiniteAIApp />);`
  }
  return `<!doctype html><html><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><style>body{margin:0;font-family:ui-sans-serif,system-ui;background:#fff;color:#111}#root{min-height:100vh}</style><script crossorigin src="https://unpkg.com/react@19/umd/react.development.js"></script><script crossorigin src="https://unpkg.com/react-dom@19/umd/react-dom.development.js"></script><script src="https://unpkg.com/@babel/standalone/babel.min.js"></script></head><body><div id="root"></div><script type="text/babel">${escapeScriptForHTML(code)}</script></body></html>`
}

function buildVuePreviewDocument(source: string) {
  const raw = String(source ?? '')
  const template = raw.match(/<template>([\s\S]*?)<\/template>/i)?.[1] ?? '<div id="vue-preview">Vue 预览</div>'
  const style = raw.match(/<style[^>]*>([\s\S]*?)<\/style>/i)?.[1] ?? ''
  let script = raw.match(/<script[^>]*>([\s\S]*?)<\/script>/i)?.[1] ?? raw
  script = script.replace(/^\s*import\s+.*?from\s+['"][^'"]+['"];?\s*$/gm, '')
  script = script.replace(/export\s+default\s+/, 'const component = ')
  if (!/const\s+component\s*=/.test(script)) {
    script = `const component = { template: ${JSON.stringify(template)} }`
  } else {
    script += `\ncomponent.template = component.template || ${JSON.stringify(template)};`
  }
  script += `\nVue.createApp(component).mount('#app');`
  return `<!doctype html><html><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><style>body{margin:0;font-family:ui-sans-serif,system-ui;background:#fff;color:#111}#app{min-height:100vh}${style}</style><script src="https://unpkg.com/vue@3/dist/vue.global.prod.js"></script></head><body><div id="app"></div><script>${escapeScriptForHTML(script)}</script></body></html>`
}

function monacoLanguageForFile(file?: ArtifactFile | null) {
  const language = String(file?.language ?? '').toLowerCase()
  if (language === 'tsx') return 'typescript'
  if (language === 'jsx' || language === 'react') return 'javascript'
  if (language === 'vue') return 'html'
  if (language === 'svg') return 'xml'
  if (language) return language
  const path = String(file?.path ?? '').toLowerCase()
  if (path.endsWith('.css')) return 'css'
  if (path.endsWith('.json')) return 'json'
  if (path.endsWith('.ts') || path.endsWith('.tsx')) return 'typescript'
  if (path.endsWith('.js') || path.endsWith('.jsx')) return 'javascript'
  if (path.endsWith('.svg')) return 'xml'
  return 'html'
}

export function UserApp() {
  const location = useLocation()
  const navigate = useNavigate()
  const publicAPIBaseURL = getPublicAPIBaseURL()
  const isExplicitNewChatView = useMemo(() => new URLSearchParams(location.search).get('new') === '1', [location.search])
  const view = useMemo(() => {
    if (location.pathname.startsWith('/share/')) return 'shared-chat'
    if (location.pathname.startsWith('/developer/docs')) return 'api-docs'
    if (location.pathname.startsWith('/developer/api')) return 'api'
    if (location.pathname.startsWith('/plans')) return 'plans'
    if (location.pathname.startsWith('/payment')) return 'payment'
    if (location.pathname.startsWith('/download')) return 'download'
    if (location.pathname.startsWith('/infinite-code')) return 'infinite-code'
    return 'chat'
  }, [location.pathname])

  const [session, setSession] = useState<SessionState | null>(null)
  const [loading, setLoading] = useState(true)
  const [theme, setTheme] = useState<ThemePreference>(() => readThemePreference())
  const [language, setLanguage] = useState('auto')
  const [chatHistoryEnabled, setChatHistoryEnabled] = useState(true)
  const [savedChatHistoryEnabled, setSavedChatHistoryEnabled] = useState(true)
  const [memoryEnabled, setMemoryEnabled] = useState(true)
  const [savedMemoryEnabled, setSavedMemoryEnabled] = useState(true)
  const [isSidebarOpen, setIsSidebarOpen] = useState(true)
  const [isMobileSidebarOpen, setIsMobileSidebarOpen] = useState(false)
  const [isUserMenuOpen, setIsUserMenuOpen] = useState(false)
  const [isModelSelectorOpen, setIsModelSelectorOpen] = useState(false)
  const [showSettingsModal, setShowSettingsModal] = useState(false)
  const [settingsTab, setSettingsTab] = useState<'general' | 'data'>('general')
  const [showLoginModal, setShowLoginModal] = useState(false)
  const [isLoginMode, setIsLoginMode] = useState(true)
  const [isPasswordResetMode, setIsPasswordResetMode] = useState(false)
  const [authForm, setAuthForm] = useState<AuthFormState>(createEmptyAuthForm())
  const [registerStep, setRegisterStep] = useState<RegisterStep>('identity')
  const [passwordResetStep, setPasswordResetStep] = useState<PasswordResetStep>('identity')
  const [registerVerificationState, setRegisterVerificationState] = useState<RegisterVerificationState>(null)
  const [passwordResetVerificationState, setPasswordResetVerificationState] = useState<RegisterVerificationState>(null)
  const [captcha, setCaptcha] = useState<CaptchaState>({ captchaId: '', imageDataUrl: '', expiresInSeconds: 0 })
  const [phoneCodeCooldown, setPhoneCodeCooldown] = useState(0)
  const [isSendingVerificationCode, setIsSendingVerificationCode] = useState(false)
  const [modelOptions, setModelOptions] = useState<ModelOption[]>(FALLBACK_MODEL_OPTIONS)
  const [selectedModel, setSelectedModel] = useState<ModelOption>(FALLBACK_MODEL_OPTIONS[0])
  const [isDeepSearch, setIsDeepSearch] = useState(false)
  const [inputMessage, setInputMessage] = useState('')
  const [isTyping, setIsTyping] = useState(false)
  const [conversations, setConversations] = useState<any[]>([])
  const [currentConversationId, setCurrentConversationId] = useState<string | null>(null)
  const [messages, setMessages] = useState<any[]>([])
  const [plans, setPlans] = useState<any[]>([])
  const [apiKeys, setApiKeys] = useState<any[]>([])
  const [downloads, setDownloads] = useState<any[]>([])
  const [subscription, setSubscription] = useState<any>(null)
  const [usage, setUsage] = useState<any>(null)
  const [copiedStates, setCopiedStates] = useState<Record<string, boolean>>({})
  const [checkoutData, setCheckoutData] = useState<any>(null)
  const [selectedPaymentMethod, setSelectedPaymentMethod] = useState('wechat')
  const [customRechargeAmount, setCustomRechargeAmount] = useState('')
  const [paymentFeedback, setPaymentFeedback] = useState('')
  const [authError, setAuthError] = useState('')
  const [chatFeedback, setChatFeedback] = useState('')
  const [shareFeedback, setShareFeedback] = useState('')
  const [editingMessageId, setEditingMessageId] = useState<string | null>(null)
  const [deletingConversationId, setDeletingConversationId] = useState<string | null>(null)
  const [pendingDeleteConversation, setPendingDeleteConversation] = useState<{ id: string; title: string } | null>(null)
  const [composerAttachments, setComposerAttachments] = useState<ComposerAttachment[]>([])
  const [isComposerDragActive, setIsComposerDragActive] = useState(false)
  const [modelLimitState, setModelLimitState] = useState<ModelLimitState | null>(null)
  const [expandedReasoningPanels, setExpandedReasoningPanels] = useState<Record<string, boolean>>({})
  const [expandedSourcePanels, setExpandedSourcePanels] = useState<Record<string, boolean>>({})
  const [previewCodeBlocks, setPreviewCodeBlocks] = useState<Record<string, boolean>>({})
  const [activeArtifact, setActiveArtifact] = useState<any | null>(null)
  const [artifactDraftFiles, setArtifactDraftFiles] = useState<ArtifactFile[]>([])
  const [activeArtifactFilePath, setActiveArtifactFilePath] = useState('')
  const [artifactStatus, setArtifactStatus] = useState('')
  const [isDeepSearchThinking, setIsDeepSearchThinking] = useState(false)
  const [showShareModal, setShowShareModal] = useState(false)
  const [isLoadingConversationShare, setIsLoadingConversationShare] = useState(false)
  const [isSavingShare, setIsSavingShare] = useState(false)
  const [conversationShare, setConversationShare] = useState<any | null>(null)
  const [shareModalState, setShareModalState] = useState<'default' | 'copy' | 'code' | 'collaboration-copy' | 'upgrade'>('default')
  const [shareResultCollaborationCode, setShareResultCollaborationCode] = useState('')
  const [shareForm, setShareForm] = useState({
    enabled: true,
    accessCode: '',
    collaborationEnabled: false,
  })
  const [sharedConversation, setSharedConversation] = useState<any | null>(null)
  const [sharedMessages, setSharedMessages] = useState<any[]>([])
  const [sharedCollaborationCodeInput, setSharedCollaborationCodeInput] = useState('')
  const [sharedCollaborationCode, setSharedCollaborationCode] = useState('')
  const [sharedCollaborationRequested, setSharedCollaborationRequested] = useState(false)
  const [sharedChatFeedback, setSharedChatFeedback] = useState('')
  const [sharedChatLoading, setSharedChatLoading] = useState(false)
  const [isJoiningSharedCollaboration, setIsJoiningSharedCollaboration] = useState(false)
  const [sharedInputMessage, setSharedInputMessage] = useState('')
  const [isSendingSharedMessage, setIsSendingSharedMessage] = useState(false)
  const [imagePreviewAsset, setImagePreviewAsset] = useState<{ url: string; fileName: string } | null>(null)
  const messagesEndRef = useRef<HTMLDivElement | null>(null)
  const fileInputRef = useRef<HTMLInputElement | null>(null)
  const imageInputRef = useRef<HTMLInputElement | null>(null)
  const composerTextareaRef = useRef<HTMLTextAreaElement | null>(null)
  const chatFeedbackTimeoutRef = useRef<number | null>(null)
  const redeemRedirectRef = useRef<string | null>(null)
  const isUserAppMountedRef = useRef(true)
  const activeRequestsRef = useRef<Record<number, ActiveUserRequest>>({})
  const requestSequenceRef = useRef(0)
  const currentConversationIdRef = useRef<string | null>(null)
  const conversationLoadSequenceRef = useRef(0)
  const conversationMessageCacheRef = useRef<Record<string, any[]>>({})
  const composerPreviewUrlsRef = useRef<Set<string>>(new Set())

  const isDark = useResolvedTheme(theme) === 'dark'
  const sharedConversationId = useMemo(() => {
    if (!location.pathname.startsWith('/share/')) {
      return ''
    }
    return decodeURIComponent(location.pathname.replace(/^\/share\//, '').split('/')[0] || '')
  }, [location.pathname])
  const oauthProviders = Array.isArray(session?.oauthProviders) ? (session.oauthProviders as OAuthProviderOption[]) : []
  const registerRequiresCaptcha = session?.authSecurity?.captchaRequiredOnRegister !== false
  const normalizedRegisterIdentifier = authForm.identifier.trim()
  const registerIdentifierIsEmail = normalizedRegisterIdentifier.includes('@')
  const registerIdentifierLabel = registerIdentifierIsEmail ? '邮箱' : '手机号'
  const colors = {
    appBg: isDark ? 'bg-[#212121]' : 'bg-white',
    sidebarBg: isDark ? 'bg-[#171717]' : 'bg-[#f9f9f9]',
    textMain: isDark ? 'text-[#ececec]' : 'text-[#0d0d0d]',
    textMuted: isDark ? 'text-[#b4b4b4]' : 'text-[#666666]',
    border: isDark ? 'border-[#333333]' : 'border-[#e5e5e5]',
    hover: isDark ? 'hover:bg-[#2f2f2f]' : 'hover:bg-[#ececec]',
    sidebarHover: isDark ? 'hover:bg-[#202123]' : 'hover:bg-[#ececec]',
    inputBg: isDark ? 'bg-[#2f2f2f]' : 'bg-[#f4f4f4]',
    userBubble: isDark ? 'bg-[#2f2f2f]' : 'bg-[#f4f4f4]',
    btnPrimary: isDark ? 'bg-white text-black hover:bg-[#ececec]' : 'bg-[#10a37f] text-white hover:bg-[#0e906f]',
    modalBg: isDark ? 'bg-[#212121]' : 'bg-white',
    modalInner: isDark ? 'bg-[#171717]' : 'bg-white',
  }
  const activePlanName = planByCode(String(subscription?.planCode ?? ''))?.name ?? getPlanLabel(String(subscription?.planCode ?? 'free'))
  const currentConversationTitle = useMemo(() => {
    return conversations.find((item) => item.id === currentConversationId)?.title || '当前聊天'
  }, [conversations, currentConversationId])
  const sharePreviewMessages = useMemo(() => {
    return messages
      .filter((item) => {
        if (!item || typeof item !== 'object') return false
        const content = normalizeVisibleMessageContent(item.content)
        const attachments = Array.isArray(item.attachments) ? item.attachments : []
        return Boolean(content || attachments.length > 0)
      })
      .slice(-4)
  }, [messages])
  const canSendSharedConversationMessage = Boolean(sharedConversation?.viewerIsOwner || sharedConversation?.viewerJoinedCollaboration || sharedCollaborationCode.trim())

  function resetAuthFlow(nextMode: 'login' | 'register' = isLoginMode ? 'login' : 'register') {
    setAuthError('')
    setAuthForm(createEmptyAuthForm())
    setCaptcha({ captchaId: '', imageDataUrl: '', expiresInSeconds: 0 })
    setPhoneCodeCooldown(0)
    setRegisterStep('identity')
    setPasswordResetStep('identity')
    setRegisterVerificationState(null)
    setPasswordResetVerificationState(null)
    setIsPasswordResetMode(false)
    setIsLoginMode(nextMode === 'login')
  }

  useEffect(() => {
    messagesEndRef.current?.scrollIntoView({ behavior: isTyping ? 'auto' : 'smooth' })
  }, [messages, isTyping])

  useEffect(() => {
    if (view !== 'shared-chat') {
      return
    }
    messagesEndRef.current?.scrollIntoView({ behavior: isSendingSharedMessage ? 'auto' : 'smooth' })
  }, [view, sharedMessages, isSendingSharedMessage])

  useEffect(() => {
    currentConversationIdRef.current = currentConversationId
  }, [currentConversationId])

  useEffect(() => {
    const activeUrls = new Set(
      composerAttachments
        .map((item) => item.previewUrl)
        .filter((url): url is string => Boolean(url?.startsWith('blob:'))),
    )
    for (const url of composerPreviewUrlsRef.current) {
      if (!activeUrls.has(url)) {
        URL.revokeObjectURL(url)
      }
    }
    composerPreviewUrlsRef.current = activeUrls
  }, [composerAttachments])

  useEffect(() => {
    return () => {
      for (const url of composerPreviewUrlsRef.current) {
        URL.revokeObjectURL(url)
      }
      composerPreviewUrlsRef.current.clear()
    }
  }, [])

  useEffect(() => {
    if (chatFeedbackTimeoutRef.current) {
      window.clearTimeout(chatFeedbackTimeoutRef.current)
      chatFeedbackTimeoutRef.current = null
    }
    if (!chatFeedback.includes('分享链接已复制')) {
      return
    }
    chatFeedbackTimeoutRef.current = window.setTimeout(() => {
      setChatFeedback((current) => (current.includes('分享链接已复制') ? '' : current))
      chatFeedbackTimeoutRef.current = null
    }, 10000)
    return () => {
      if (chatFeedbackTimeoutRef.current) {
        window.clearTimeout(chatFeedbackTimeoutRef.current)
        chatFeedbackTimeoutRef.current = null
      }
    }
  }, [chatFeedback])

  useEffect(() => {
    if (!currentConversationId) {
      return
    }
    conversationMessageCacheRef.current[currentConversationId] = messages
  }, [currentConversationId, messages])

  function isSameConversationKey(left: string | null | undefined, right: string | null | undefined) {
    return String(left ?? '') === String(right ?? '')
  }

  function listActiveRequests() {
    return Object.values(activeRequestsRef.current)
  }

  function getActiveRequestForConversation(conversationId: string | null | undefined) {
    const matches = listActiveRequests()
      .filter((item) => isSameConversationKey(item.conversationId, conversationId))
      .sort((left, right) => right.id - left.id)
    return matches[0] ?? null
  }

  function getVisibleActiveRequestSnapshot() {
    const activeForRenderedConversation = getActiveRequestForConversation(currentConversationId)
    if (activeForRenderedConversation) {
      return activeForRenderedConversation
    }
    const activeForCurrentRef = getActiveRequestForConversation(currentConversationIdRef.current)
    if (activeForCurrentRef && isTyping) {
      return activeForCurrentRef
    }
    const activeRequests = listActiveRequests()
    if (isTyping && activeRequests.length === 1) {
      return activeRequests[0] ?? null
    }
    return null
  }

  function getActiveRequestByRunId(runId: string | null | undefined) {
    if (!runId) {
      return null
    }
    return listActiveRequests().find((item) => item.runId === runId) ?? null
  }

  function hasActiveRequestForConversation(conversationId: string | null | undefined) {
    return Boolean(getActiveRequestForConversation(conversationId))
  }

  function syncVisibleRequestState(conversationId: string | null | undefined = currentConversationIdRef.current) {
    const activeRequest = getActiveRequestForConversation(conversationId)
    setIsTyping(Boolean(activeRequest))
    setIsDeepSearchThinking(Boolean(activeRequest?.isDeepSearch && activeRequest.phase === 'thinking'))
  }

  function applyLocalStoppedState(activeRequest: ActiveUserRequest) {
    const streamIds = new Set<string>()
    streamIds.add(`stream-${activeRequest.conversationId ?? 'temporary'}-${activeRequest.id}`)
    if (activeRequest.runId) {
      streamIds.add(`stream-${activeRequest.runId}`)
    }
    const noticeModelSlug = selectedModel?.slug ?? FALLBACK_MODEL_OPTIONS[0].slug
    const updateMessages = (prev: any[]) => {
      let updatedStreamingMessage = false
      const next = prev.flatMap((item) => {
        const itemId = String(item?.id ?? '')
        if (isPendingImagePlaceholder(item)) {
          return []
        }
        if (streamIds.has(itemId) && isAssistantRole(item?.role)) {
          updatedStreamingMessage = true
          const content = String(item?.content ?? '').trim()
          return [{
            ...item,
            content: content ? `${content}\n\n（已停止输出）` : '已停止输出。',
            attachments: [],
            optimistic: true,
          }]
        }
        return [item]
      })
      if (updatedStreamingMessage) {
        return next
      }
      return [...next, createLocalAssistantNoticeMessage('已停止输出。', noticeModelSlug)]
    }
    if (activeRequest.conversationId) {
      applyMessagesForConversation(activeRequest.conversationId, updateMessages)
      return
    }
    if (isViewingConversation(null)) {
      setMessages(updateMessages)
    }
  }

  function cancelMatchingRequests(predicate: (request: ActiveUserRequest) => boolean, options: { resetState?: boolean; waitForServer?: boolean } = {}) {
    let didCancel = false
    for (const activeRequest of listActiveRequests()) {
      if (!predicate(activeRequest)) {
        continue
      }
      didCancel = true
      const cancelPromise = activeRequest.runId ? api.cancelChatRun(activeRequest.runId).catch(() => undefined) : Promise.resolve(undefined)
      delete activeRequestsRef.current[activeRequest.id]
      if (options.waitForServer) {
        applyLocalStoppedState(activeRequest)
        if (activeRequest.conversationId) {
          void cancelPromise.finally(() => {
            window.setTimeout(() => {
              void refreshConversationMessagesFromServer(activeRequest.conversationId as string).catch(() => undefined)
            }, 1000)
          })
        }
      } else {
        void cancelPromise
      }
      activeRequest.controller.abort()
    }
    if (didCancel && options.resetState !== false) {
      syncVisibleRequestState()
      return
    }
    if (didCancel) {
      syncVisibleRequestState()
    }
  }

  function cancelConversationRequest(conversationId: string | null, options: { resetState?: boolean; waitForServer?: boolean } = {}) {
    cancelMatchingRequests((request) => isSameConversationKey(request.conversationId, conversationId), options)
  }

  function cancelActiveRequest(options: { resetState?: boolean; waitForServer?: boolean } = {}) {
    cancelMatchingRequests(() => true, options)
  }

  function cancelVisibleRequest(options: { resetState?: boolean; waitForServer?: boolean } = {}) {
    const activeRequest = getVisibleActiveRequestSnapshot()
    if (!activeRequest) {
      syncVisibleRequestState()
      return
    }
    cancelMatchingRequests((request) => request.id === activeRequest.id, options)
  }

  function startActiveRequest(conversationId: string | null, options: { isDeepSearch?: boolean; cancelExisting?: boolean } = {}) {
    if (options.cancelExisting !== false) {
      cancelConversationRequest(conversationId, { resetState: false })
    }
    const controller = new AbortController()
    const nextRequest: ActiveUserRequest = {
      id: requestSequenceRef.current + 1,
      controller,
      conversationId,
      isDeepSearch: Boolean(options.isDeepSearch),
      phase: options.isDeepSearch ? 'thinking' : 'answering',
    }
    requestSequenceRef.current = nextRequest.id
    activeRequestsRef.current[nextRequest.id] = nextRequest
    syncVisibleRequestState()
    return nextRequest
  }

  function isActiveRequest(requestId: number) {
    return Boolean(activeRequestsRef.current[requestId])
  }

  function updateActiveRequest(requestId: number, update: Partial<ActiveUserRequest>) {
    const activeRequest = activeRequestsRef.current[requestId]
    if (!activeRequest) {
      return null
    }
    const nextRequest = { ...activeRequest, ...update }
    activeRequestsRef.current[requestId] = nextRequest
    syncVisibleRequestState()
    return nextRequest
  }

  function isViewingConversation(conversationId: string | null | undefined) {
    return String(currentConversationIdRef.current ?? '') === String(conversationId ?? '')
  }

  function applyMessagesForConversation(conversationId: string | null | undefined, update: MessageUpdater) {
    if (!conversationId) {
      return
    }
    const previous = Array.isArray(conversationMessageCacheRef.current[conversationId]) ? conversationMessageCacheRef.current[conversationId] : []
    const next = typeof update === 'function' ? (update as (previous: any[]) => any[])(previous) : update
    conversationMessageCacheRef.current[conversationId] = next
    if (isViewingConversation(conversationId)) {
      setMessages(next)
    }
  }

  function preserveActiveConversationRequestOnNavigation() {
    const activeRequest = getActiveRequestForConversation(currentConversationIdRef.current)
    if (!activeRequest) {
      return
    }
    if (!activeRequest.conversationId) {
      cancelConversationRequest(null)
      return
    }
    setIsTyping(false)
    setIsDeepSearchThinking(false)
  }

  function finishActiveRequest(requestId: number) {
    if (!activeRequestsRef.current[requestId]) {
      return false
    }
    delete activeRequestsRef.current[requestId]
    syncVisibleRequestState()
    return true
  }

  async function refreshConversationMessagesFromServer(conversationId: string) {
    const response = await api.listMessages(conversationId)
    const persistedMessages = Array.isArray(response.messages) ? response.messages : []
    conversationMessageCacheRef.current[conversationId] = persistedMessages
    if (currentConversationIdRef.current === conversationId) {
      setMessages(persistedMessages)
    }
  }

  async function settleRunFromServer(runId: string, conversationId: string, requestId: number) {
    try {
      const response = await api.getChatRun(runId)
      if (!isActiveRequest(requestId)) {
        return false
      }
      const status = String(response.run?.status ?? '').toLowerCase()
      if (!['completed', 'failed', 'canceled'].includes(status)) {
        return false
      }
      await refreshConversationMessagesFromServer(conversationId)
      finishActiveRequest(requestId)
      return true
    } catch (error) {
      if (!isActiveRequest(requestId)) {
        return false
      }
      if (isNotFoundRequestError(error)) {
        await refreshConversationMessagesFromServer(conversationId)
        finishActiveRequest(requestId)
        return true
      }
      return false
    }
  }

  async function reconcileActiveRunForConversation(conversationId: string | null | undefined, requestId: number) {
    if (!conversationId || !isActiveRequest(requestId)) {
      return
    }
    const activeRequest = activeRequestsRef.current[requestId]
    if (!activeRequest?.runId) {
      return
    }
    try {
      const response = await api.listActiveChatRuns(conversationId)
      if (!isActiveRequest(requestId)) {
        return
      }
      const activeRunIds = new Set((Array.isArray(response.runs) ? response.runs : []).map((run: any) => String(run?.id ?? '')).filter(Boolean))
      if (activeRunIds.has(activeRequest.runId)) {
        return
      }
      await refreshConversationMessagesFromServer(conversationId)
      finishActiveRequest(requestId)
    } catch {
      // 对账只是兜底保护，失败时继续等待事件流，避免误清正在输出的任务。
    }
  }

  useEffect(() => {
    const reconcileTimer = window.setInterval(() => {
      const activeRequest = getActiveRequestForConversation(currentConversationIdRef.current)
      if (activeRequest?.conversationId && activeRequest.runId) {
        void reconcileActiveRunForConversation(activeRequest.conversationId, activeRequest.id)
      }
    }, 4000)
    return () => {
      window.clearInterval(reconcileTimer)
      isUserAppMountedRef.current = false
      for (const activeRequest of listActiveRequests()) {
        activeRequest.controller.abort()
      }
      activeRequestsRef.current = {}
    }
  }, [])

  useEffect(() => {
    void bootstrap()
  }, [])

  useEffect(() => {
    const titleByView: Record<string, string> = {
      chat: 'Infinite-AI 智能助手',
      'shared-chat': 'Infinite-AI 对话分享',
      plans: 'Infinite-AI 套餐中心',
      payment: 'Infinite-AI 支付',
      api: 'Infinite-AI 开发者平台',
      'api-docs': 'Infinite-AI 开发者平台',
      download: 'Infinite-AI 下载中心',
      'infinite-code': 'Infinite Code - Infinite-AI',
    }
    document.title = titleByView[view] || 'Infinite-AI 智能助手'
  }, [view])

  useEffect(() => {
    if (view !== 'shared-chat' || !sharedConversationId) {
      setSharedConversation(null)
      setSharedMessages([])
      setSharedChatFeedback('')
      setSharedCollaborationCode('')
      setSharedCollaborationCodeInput('')
      setSharedCollaborationRequested(false)
      return
    }
  }, [view, sharedConversationId, location.search])

  useEffect(() => {
    syncVisibleRequestState(currentConversationId)
  }, [currentConversationId, chatHistoryEnabled])

  useEffect(() => {
    if (chatHistoryEnabled && currentConversationId && session?.user) {
      void loadMessages(currentConversationId)
    } else {
      conversationLoadSequenceRef.current += 1
      setMessages([])
    }
  }, [chatHistoryEnabled, currentConversationId, session?.user])

  useEffect(() => {
    if (phoneCodeCooldown <= 0) return
    const timer = window.setTimeout(() => setPhoneCodeCooldown((prev) => Math.max(0, prev - 1)), 1000)
    return () => window.clearTimeout(timer)
  }, [phoneCodeCooldown])

  useEffect(() => {
    if (view !== 'shared-chat' || !sharedConversationId) {
      return
    }
    void loadSharedConversation(sharedConversationId)
  }, [view, sharedConversationId, session?.user?.id])

  useEffect(() => {
    if (editingMessageId && !messages.some((item) => item.id === editingMessageId)) {
      setEditingMessageId(null)
    }
  }, [editingMessageId, messages])

  useEffect(() => {
    if (!session?.user || !chatHistoryEnabled || !currentConversationId || hasActiveRequestForConversation(currentConversationId)) {
      return
    }
    const lastMessage = messages[messages.length - 1]
    if (!lastMessage || lastMessage.optimistic || normalizeProviderRoleForClient(lastMessage.role) !== 'user') {
      return
    }
    const timer = window.setInterval(() => {
      void loadMessages(currentConversationId)
    }, 2500)
    return () => window.clearInterval(timer)
  }, [session?.user, chatHistoryEnabled, currentConversationId, messages])

  function applyConversationTitleLocally(conversationId: string | null | undefined, content: string, force = false) {
    if (!conversationId) {
      return
    }
    const nextTitle = deriveConversationTitle(content)
    if (!nextTitle || nextTitle === '新聊天') {
      return
    }
    setConversations((prev) =>
      prev.map((item) => {
        if (item.id !== conversationId) {
          return item
        }
        if (!force && !isGenericConversationTitle(item.title)) {
          return item
        }
        return { ...item, title: nextTitle }
      }),
    )
  }

  function openReasoningPanel(messageId: string) {
    setExpandedReasoningPanels((prev) => ({ ...prev, [messageId]: true }))
  }

  function upsertStreamingAssistantText(messageId: string, field: StreamingAssistantField, chunk: string, conversationId?: string | null) {
    if (!chunk) {
      return
    }
    const updateMessages = (prev: any[]) => {
      const nextValueByField = (item: any) => `${String(item?.[field] ?? '')}${chunk}`
      const exists = prev.some((item) => item.id === messageId)
      if (!exists) {
        return [
          ...prev,
          {
            id: messageId,
            role: 'assistant',
            content: field === 'content' ? chunk : '',
            reasoningContent: field === 'reasoningContent' ? chunk : '',
          },
        ]
      }
      return prev.map((item) => (item.id === messageId ? { ...item, [field]: nextValueByField(item) } : item))
    }
    if (conversationId) {
      applyMessagesForConversation(conversationId, updateMessages)
      return
    }
    setMessages(updateMessages)
  }

  function upsertStreamingAssistantSources(messageId: string, sources: any[], conversationId?: string | null) {
    if (!Array.isArray(sources) || sources.length === 0) {
      return
    }
    const updateMessages = (prev: any[]) => {
      const exists = prev.some((item) => item.id === messageId)
      if (!exists) {
        return [
          ...prev,
          {
            id: messageId,
            role: 'assistant',
            content: '',
            reasoningContent: '',
            sources,
          },
        ]
      }
      return prev.map((item) => (item.id === messageId ? { ...item, sources } : item))
    }
    if (conversationId) {
      applyMessagesForConversation(conversationId, updateMessages)
      return
    }
    setMessages(updateMessages)
  }

  function getTypewriterBatchSize(totalChars: number, field: StreamingAssistantField) {
    if (totalChars <= 140) {
      return 1
    }
    if (totalChars <= 320) {
      return field === 'reasoningContent' ? 1 : 2
    }
    if (totalChars <= 720) {
      return field === 'reasoningContent' ? 2 : 3
    }
    if (totalChars <= 1400) {
      return field === 'reasoningContent' ? 3 : 4
    }
    return field === 'reasoningContent' ? 4 : 5
  }

  function getTypewriterDelay(totalChars: number, field: StreamingAssistantField) {
    if (totalChars <= 140) {
      return field === 'reasoningContent' ? 38 : 32
    }
    if (totalChars <= 320) {
      return field === 'reasoningContent' ? 34 : 28
    }
    if (totalChars <= 720) {
      return field === 'reasoningContent' ? 28 : 22
    }
    return field === 'reasoningContent' ? 22 : 18
  }

  function getTypewriterPause(chunk: string, field: StreamingAssistantField) {
    const tail = [...chunk].at(-1) ?? ''
    if (!tail) {
      return 0
    }
    if (/[。！？!?]/.test(tail)) {
      return field === 'reasoningContent' ? 120 : 90
    }
    if (/[，、；;：:\n]/.test(tail)) {
      return field === 'reasoningContent' ? 70 : 50
    }
    return 0
  }

  async function animateStreamingText(messageId: string, field: StreamingAssistantField, value: string, requestId?: number, conversationId?: string | null) {
    const characters = [...String(value ?? '')]
    if (characters.length === 0) {
      return
    }
    const batchSize = getTypewriterBatchSize(characters.length, field)
    const delay = getTypewriterDelay(characters.length, field)
    for (let index = 0; index < characters.length; index += batchSize) {
      if (!isUserAppMountedRef.current || (requestId && !isActiveRequest(requestId))) {
        return
      }
      const nextChunk = characters.slice(index, index + batchSize).join('')
      flushSync(() => {
        upsertStreamingAssistantText(messageId, field, nextChunk, conversationId)
      })
      if (index + batchSize < characters.length) {
        await new Promise<void>((resolve) => window.requestAnimationFrame(() => resolve()))
        await new Promise<void>((resolve) => window.setTimeout(resolve, delay + getTypewriterPause(nextChunk, field)))
        if (requestId && !isActiveRequest(requestId)) {
          return
        }
      }
    }
  }

  useEffect(() => {
    storeSelectedModelSlug(selectedModel?.slug ?? '')
  }, [selectedModel])

  function applyConversationState(conversationId: string | null, availableConversations: any[] = conversations) {
    if (!conversationId) {
      const storedDeepSearchPreference = readStoredDeepSearchPreference()
      if (storedDeepSearchPreference !== null) {
        setIsDeepSearch(storedDeepSearchPreference)
      }
      return
    }
    const conversation = availableConversations.find((item) => item.id === conversationId)
    if (!conversation) {
      return
    }
    storeDeepSearchPreference(Boolean(conversation.deepSearch))
    setIsDeepSearch(Boolean(conversation.deepSearch))
    if (conversation.modelSlug) {
      const matchedModel = modelOptions.find((item) => item.slug === conversation.modelSlug)
      if (matchedModel) {
        setSelectedModel(matchedModel)
      }
    }
  }

  useEffect(() => {
    const params = new URLSearchParams(location.search)
    const authMode = params.get('auth')
    if (location.pathname === '/register' || authMode === 'register') {
      setIsLoginMode(false)
      setShowLoginModal(true)
      return
    }
    if (location.pathname === '/login' || authMode === 'login') {
      setIsLoginMode(true)
      setShowLoginModal(true)
    }
  }, [location.pathname, location.search])

  useEffect(() => {
    if (!showLoginModal || isLoginMode || registerStep !== 'captcha' || !registerRequiresCaptcha) return
    if (!captcha.captchaId) {
      void loadCaptcha()
    }
  }, [showLoginModal, isLoginMode, registerStep, registerRequiresCaptcha, captcha.captchaId])

  useEffect(() => {
    if (!showLoginModal || (!isLoginMode && !isPasswordResetMode)) return
    if (!captcha.captchaId) {
      void loadCaptcha()
    }
  }, [showLoginModal, isLoginMode, isPasswordResetMode, passwordResetStep, captcha.captchaId])

  useEffect(() => {
    const params = new URLSearchParams(location.search)
    const redeemCode = params.get('redeem')
    if (!session?.user) return
    if (redeemCode) {
      if (redeemRedirectRef.current === redeemCode) return
      redeemRedirectRef.current = redeemCode
      void (async () => {
        try {
          await api.redeemClaim(redeemCode)
          navigate(`/redeem?code=${encodeURIComponent(redeemCode)}&claimed=1`, { replace: true })
        } catch (error) {
          setAuthError(error instanceof Error ? error.message : '兑换失败')
          navigate(`/redeem?code=${encodeURIComponent(redeemCode)}`, { replace: true })
        } finally {
          redeemRedirectRef.current = null
        }
      })()
      return
    }
    if (location.pathname === '/login' || location.pathname === '/register') {
      navigate('/', { replace: true })
    }
  }, [session?.user, location.pathname, location.search, navigate])

  async function bootstrap() {
    try {
      setLoading(true)
      const routeSearch = typeof window !== 'undefined' ? window.location.search : location.search
      const routeParams = new URLSearchParams(routeSearch)
      const requestedConversationId = routeParams.get('c')
      const explicitNewChat = routeParams.get('new') === '1'
      const currentSession = await api.getSession()
      setSession(currentSession)
      const storedDeepSearchPreference = readStoredDeepSearchPreference()
      if (!currentSession.user) {
        setChatHistoryEnabled(true)
        setSavedChatHistoryEnabled(true)
        setMemoryEnabled(true)
        setSavedMemoryEnabled(true)
        conversationMessageCacheRef.current = {}
        setConversations([])
        setCurrentConversationId(null)
        setMessages([])
        setIsDeepSearch(storedDeepSearchPreference ?? false)
      }
      const settings = currentSession.user ? await api.getUserSettings().catch(() => null) : null
      if (settings) {
        setTheme(normalizeThemePreference(settings.theme))
        setLanguage(settings.language || 'auto')
        setIsDeepSearch(storedDeepSearchPreference ?? Boolean(settings.deepSearchDefault))
        const nextChatHistoryEnabled = settings.chatHistoryEnabled !== false
        const nextMemoryEnabled = settings.memoryEnabled !== false
        setChatHistoryEnabled(nextChatHistoryEnabled)
        setSavedChatHistoryEnabled(nextChatHistoryEnabled)
        setMemoryEnabled(nextMemoryEnabled)
        setSavedMemoryEnabled(nextMemoryEnabled)
      }
      const [planResponse, downloadResponse, modelResponse] = await Promise.all([api.listPlans(), api.listDownloads(), api.listChatModels().catch(() => ({ models: FALLBACK_MODEL_OPTIONS }))])
      setPlans(Array.isArray(planResponse.plans) ? planResponse.plans : [])
      setDownloads(Array.isArray(downloadResponse.releases) ? downloadResponse.releases : [])
      const nextModels = Array.isArray(modelResponse.models) && modelResponse.models.length > 0 ? modelResponse.models : FALLBACK_MODEL_OPTIONS
      const preferredModelSlug = String(settings?.selectedModelSlug || readStoredSelectedModelSlug() || '')
      const nextChatHistoryEnabled = settings?.chatHistoryEnabled !== false
      setModelOptions(nextModels)
      setSelectedModel((prev) => nextModels.find((item: any) => item.slug === preferredModelSlug) ?? nextModels.find((item: any) => item.slug === prev?.slug) ?? nextModels[0])
      if (currentSession.user) {
        const [conversationResponse, keyResponse, subResponse, usageResponse] = await Promise.all([
          nextChatHistoryEnabled ? api.listConversations() : Promise.resolve({ conversations: [] }),
          api.listApiKeys(),
          api.getSubscription().catch(() => null),
          api.developerUsage().catch(() => null),
        ])
        const nextConversations: any[] = Array.isArray(conversationResponse.conversations) ? conversationResponse.conversations : []
        setConversations(nextConversations)
        setApiKeys(Array.isArray(keyResponse.apiKeys) ? keyResponse.apiKeys : [])
        setSubscription(subResponse)
        setUsage(usageResponse)
        if (!nextChatHistoryEnabled || explicitNewChat) {
          setCurrentConversationId(null)
          setMessages([])
          setIsDeepSearch(storedDeepSearchPreference ?? Boolean(settings?.deepSearchDefault))
        } else if (requestedConversationId && nextConversations.some((item) => item.id === requestedConversationId)) {
          setCurrentConversationId(requestedConversationId)
          const requestedConversation = nextConversations.find((item) => item.id === requestedConversationId)
          if (requestedConversation) {
            setIsDeepSearch(Boolean(requestedConversation.deepSearch))
            const matchedModel = nextModels.find((item: any) => item.slug === requestedConversation.modelSlug)
            if (matchedModel) {
              setSelectedModel(matchedModel)
            }
          }
        } else if (nextConversations[0]?.id) {
          const fallbackConversationId = nextConversations[0].id
          setCurrentConversationId(fallbackConversationId)
          setIsDeepSearch(Boolean(nextConversations[0].deepSearch))
          const matchedModel = nextModels.find((item: any) => item.slug === nextConversations[0].modelSlug)
          if (matchedModel) {
            setSelectedModel(matchedModel)
          }
          if (requestedConversationId && requestedConversationId !== fallbackConversationId) {
            navigate(buildChatRoute(fallbackConversationId), { replace: true })
          }
        } else {
          setCurrentConversationId(null)
          setIsDeepSearch(storedDeepSearchPreference ?? Boolean(settings?.deepSearchDefault))
          if (requestedConversationId) {
            navigate(buildChatRoute(null), { replace: true })
          }
        }
      }
    } catch (error) {
      setChatFeedback(error instanceof Error ? error.message : '页面初始化失败，请刷新后重试。')
    } finally {
      setLoading(false)
    }
  }

  function findRecoveredAssistantMessage(previousMessages: any[], persistedMessages: any[]) {
    if (!Array.isArray(previousMessages) || !Array.isArray(persistedMessages) || previousMessages.length === 0 || persistedMessages.length === 0) {
      return null
    }
    const previousLastMessage = previousMessages[previousMessages.length - 1]
    if (!previousLastMessage || previousLastMessage.optimistic || normalizeProviderRoleForClient(previousLastMessage.role) !== 'user') {
      return null
    }
    const previousLastIndex = persistedMessages.findIndex((item) => item?.id === previousLastMessage.id)
    if (previousLastIndex < 0 || previousLastIndex >= persistedMessages.length - 1) {
      return null
    }
    const addedMessages = persistedMessages.slice(previousLastIndex + 1)
    const recoveredAssistant = addedMessages.find((item) => isAssistantRole(item?.role))
    if (!recoveredAssistant || !recoveredAssistant.id) {
      return null
    }
    return recoveredAssistant
  }

  async function animateRecoveredAssistantMessage(conversationId: string, persistedMessages: any[], recoveredAssistant: any, loadRequestId: number) {
    const assistantId = recoveredAssistant.id
    const reasoning = String(recoveredAssistant.reasoningContent ?? '')
    const content = String(recoveredAssistant.content ?? '')
    const animatedMessages = persistedMessages.map((item) =>
      item?.id === assistantId
        ? {
            ...item,
            reasoningContent: '',
            content: '',
          }
        : item,
    )
    conversationMessageCacheRef.current[conversationId] = animatedMessages
    if (currentConversationIdRef.current === conversationId && conversationLoadSequenceRef.current === loadRequestId) {
      setMessages(animatedMessages)
      if (reasoning.trim()) {
        openReasoningPanel(assistantId)
      }
    }
    if (reasoning) {
      await animateStreamingText(assistantId, 'reasoningContent', reasoning, undefined, conversationId)
    }
    if (content) {
      await animateStreamingText(assistantId, 'content', content, undefined, conversationId)
    }
    if (conversationLoadSequenceRef.current !== loadRequestId) {
      return
    }
    conversationMessageCacheRef.current[conversationId] = persistedMessages
    if (currentConversationIdRef.current === conversationId) {
      setMessages(persistedMessages)
    }
  }

  async function resumeActiveRunsForConversation(conversationId: string, loadRequestId: number) {
    try {
      const response = await api.listActiveChatRuns(conversationId)
      if (conversationLoadSequenceRef.current !== loadRequestId || currentConversationIdRef.current !== conversationId) {
        return
      }
      const runs = (Array.isArray(response.runs) ? response.runs : []) as any[]
      for (const run of runs) {
        const runId = String(run?.id ?? '')
        if (!runId || getActiveRequestByRunId(runId)) {
          continue
        }
        const activeRequest = startActiveRequest(conversationId, {
          isDeepSearch: Boolean(run?.deepSearch),
          cancelExisting: false,
        })
        updateActiveRequest(activeRequest.id, { runId, phase: run?.deepSearch ? 'thinking' : 'answering' })
        void subscribeToRunEvents(runId, conversationId, activeRequest.id, 0)
      }
    } catch {
      // 主消息加载不应被运行态恢复影响，用户刷新后仍能看到已保存内容。
    }
  }

  async function loadMessages(conversationId: string) {
    const loadRequestId = conversationLoadSequenceRef.current + 1
    conversationLoadSequenceRef.current = loadRequestId
    try {
      const response = await api.listMessages(conversationId)
      if (conversationLoadSequenceRef.current !== loadRequestId || currentConversationIdRef.current !== conversationId) {
        return
      }
      const persistedMessages = Array.isArray(response.messages) ? response.messages : []
      const cachedMessages = conversationMessageCacheRef.current[conversationId]
      const shouldPreferCachedMessages = Boolean(hasActiveRequestForConversation(conversationId) && Array.isArray(cachedMessages) && cachedMessages.length > 0)
      const nextMessages = shouldPreferCachedMessages ? cachedMessages : persistedMessages
      if (!shouldPreferCachedMessages) {
        const recoveredAssistant = findRecoveredAssistantMessage(Array.isArray(cachedMessages) ? cachedMessages : [], persistedMessages)
        if (recoveredAssistant) {
          await animateRecoveredAssistantMessage(conversationId, persistedMessages, recoveredAssistant, loadRequestId)
          return
        }
      }
      conversationMessageCacheRef.current[conversationId] = nextMessages
      setMessages(nextMessages)
      void resumeActiveRunsForConversation(conversationId, loadRequestId)
    } catch (error) {
      if (isNotFoundRequestError(error) && currentConversationIdRef.current === conversationId) {
        delete conversationMessageCacheRef.current[conversationId]
        currentConversationIdRef.current = null
        setCurrentConversationId(null)
        setMessages([])
        setEditingMessageId(null)
        setComposerAttachments([])
        setModelLimitState(null)
        setChatFeedback('这条聊天不存在或已被删除，已为你切回新聊天。')
        applyConversationState(null)
        navigate(buildChatRoute(null), { replace: true })
        return
      }
      setChatFeedback(error instanceof Error ? error.message : '聊天记录加载失败，请刷新后重试。')
    }
  }

  async function loadConversationShare(conversationId: string) {
    const response = await api.getConversationShare(conversationId)
    const share = response.share
    setConversationShare(share)
    if (share) {
      setShareForm({
        enabled: share.id ? share.isActive !== false : true,
        accessCode: share.accessCode ?? '',
        collaborationEnabled: Boolean(share.collaborationEnabled),
      })
      setShareModalState('default')
      return share
    }
    setConversationShare(null)
    setShareForm({
      enabled: true,
      accessCode: '',
      collaborationEnabled: false,
    })
    setShareModalState('default')
    return null
  }

  async function openShareModal() {
    if (!currentConversationId) {
      return
    }
    setIsLoadingConversationShare(true)
    setShareFeedback('')
    setConversationShare(null)
    setShareModalState('default')
    setShareResultCollaborationCode('')
    setShowShareModal(true)
    try {
      await loadConversationShare(currentConversationId)
    } catch (error) {
      setShareFeedback(error instanceof Error ? error.message : '分享设置加载失败，请稍后重试。')
    } finally {
      setIsLoadingConversationShare(false)
    }
  }

  async function saveConversationShare(overrides?: Partial<{ enabled: boolean; accessCode: string; collaborationEnabled: boolean }>) {
    if (!currentConversationId) {
      return null
    }
    setIsSavingShare(true)
    setShareFeedback('')
    const nextEnabled = overrides?.enabled ?? shareForm.enabled
    const nextAccessCode = overrides?.accessCode ?? shareForm.accessCode
    const nextCollaborationEnabled = overrides?.collaborationEnabled ?? shareForm.collaborationEnabled
    try {
      const response = await api.updateConversationShare(currentConversationId, {
        enabled: Boolean(nextEnabled),
        requireAccessCode: false,
        accessCode: nextAccessCode,
        collaborationEnabled: Boolean(nextCollaborationEnabled),
      })
      const share = response.share
      setConversationShare(share)
      setShareForm({
        enabled: share?.isActive !== false,
        accessCode: share?.accessCode ?? '',
        collaborationEnabled: Boolean(share?.collaborationEnabled),
      })
      return share ?? null
    } catch (error) {
      setShareFeedback(error instanceof Error ? error.message : '分享设置保存失败，请稍后重试。')
      return null
    } finally {
      setIsSavingShare(false)
    }
  }

  async function handlePrepareConversationShareLink() {
    const share = await saveConversationShare({ enabled: true })
    if (share?.shareURL) {
      setShareModalState('copy')
      setShareFeedback('')
    }
  }

  async function handlePrepareCollaborationShareLink() {
    const code = shareForm.accessCode.trim()
    if (!code) {
      setShareFeedback('请输入协作码。')
      return
    }
    const share = await saveConversationShare({
      enabled: true,
      collaborationEnabled: true,
      accessCode: code,
    })
    if (share?.shareURL) {
      setShareResultCollaborationCode(code)
      setShareModalState('collaboration-copy')
      setShareFeedback('')
    }
  }

  async function ensureConversationShareLink() {
    if (!currentConversationId) {
      return ''
    }
    try {
      const existingShare =
        conversationShare?.shareURL && conversationShare?.isActive !== false
          ? conversationShare
          : (await api.getConversationShare(currentConversationId)).share
      if (existingShare?.shareURL && existingShare?.isActive !== false) {
        setConversationShare(existingShare)
        setShareForm({
          enabled: existingShare.isActive !== false,
          accessCode: existingShare.accessCode ?? '',
          collaborationEnabled: Boolean(existingShare.collaborationEnabled),
        })
        return String(existingShare.shareURL)
      }
      const response = await api.updateConversationShare(currentConversationId, {
        enabled: true,
        requireAccessCode: false,
        accessCode: existingShare?.accessCode ?? shareForm.accessCode,
        collaborationEnabled: Boolean(existingShare?.collaborationEnabled ?? shareForm.collaborationEnabled),
      })
      const share = response.share
      setConversationShare(share)
      setShareForm({
        enabled: share?.isActive !== false,
        accessCode: share?.accessCode ?? '',
        collaborationEnabled: Boolean(share?.collaborationEnabled),
      })
      return String(share?.shareURL ?? '')
    } catch (error) {
      setChatFeedback(error instanceof Error ? error.message : '分享链接生成失败，请稍后重试。')
      return ''
    }
  }

  async function loadSharedConversation(shareId: string) {
    if (!shareId) {
      return
    }
    setSharedChatLoading(true)
    setSharedChatFeedback('')
    try {
      const response = await api.getPublicConversationShare(shareId)
      setSharedConversation(response.share)
      setSharedMessages(Array.isArray(response.messages) ? response.messages : [])
    } catch (error) {
      setSharedConversation(null)
      setSharedMessages([])
      setSharedChatFeedback(error instanceof Error ? error.message : '分享加载失败，请稍后重试。')
    } finally {
      setSharedChatLoading(false)
    }
  }

  async function handleJoinSharedCollaboration() {
    if (!sharedConversationId || !sharedConversation) {
      return
    }
    if (!session?.user) {
      setIsLoginMode(true)
      setShowLoginModal(true)
      return
    }
    setSharedCollaborationRequested(true)
    const code = sharedCollaborationCodeInput.trim()
    if (!code) {
      setSharedChatFeedback('请输入协作码。')
      return
    }
    setIsJoiningSharedCollaboration(true)
    setSharedChatFeedback('')
    try {
      await api.joinSharedConversationCollaboration(sharedConversationId, { collaborationCode: code })
      setSharedCollaborationCode(code)
      await loadSharedConversation(sharedConversationId)
      setSharedChatFeedback('已加入协作，现在可以继续对话。')
    } catch (error) {
      setSharedChatFeedback(error instanceof Error ? error.message : '加入协作失败，请稍后重试。')
    } finally {
      setIsJoiningSharedCollaboration(false)
    }
  }

  async function handleSendSharedMessage() {
    if (!sharedConversationId || !sharedConversation) {
      return
    }
    if (!session?.user) {
      setIsLoginMode(true)
      setShowLoginModal(true)
      return
    }
    const content = sharedInputMessage.trim()
    if (!content || isSendingSharedMessage) {
      return
    }
    const optimisticUserMessage = createOptimisticUserMessage(content, sharedConversation.modelSlug ?? 'infinite-ai-standard')
    setSharedMessages((prev) => [...prev, optimisticUserMessage])
    setSharedInputMessage('')
    setSharedChatFeedback('')
    setIsSendingSharedMessage(true)
    try {
      const response = await api.sendSharedConversationMessage(sharedConversationId, {
        content,
        collaborationCode: sharedCollaborationCode,
      })
      setSharedMessages((prev) => [
        ...prev.filter((item) => item.id !== optimisticUserMessage.id),
        response.userMessage,
        response.assistantMessage,
      ])
      if (response.title) {
        setSharedConversation((prev: any) => prev ? { ...prev, title: response.title } : prev)
      }
    } catch (error) {
      setSharedMessages((prev) => prev.filter((item) => item.id !== optimisticUserMessage.id))
      setSharedChatFeedback(error instanceof Error ? error.message : '协作发送失败，请稍后重试。')
    } finally {
      setIsSendingSharedMessage(false)
    }
  }

  function navigateTo(targetView: string) {
    const map: Record<string, string> = {
      chat: buildChatRoute(currentConversationId),
      plans: '/plans',
      payment: '/payment',
      api: '/developer/api',
      'api-docs': '/developer/docs',
      download: '/download',
      'infinite-code': '/infinite-code',
    }
    navigate(map[targetView] ?? '/')
    setIsMobileSidebarOpen(false)
    setIsUserMenuOpen(false)
  }

  function startNewChat() {
    preserveActiveConversationRequestOnNavigation()
    currentConversationIdRef.current = null
    setCurrentConversationId(null)
    setMessages([])
    setEditingMessageId(null)
    setComposerAttachments([])
    setInputMessage('')
    setChatFeedback('')
    setModelLimitState(null)
    applyConversationState(null)
    navigate(buildChatRoute(null))
    setIsMobileSidebarOpen(false)
  }

  function openConversation(conversationId: string) {
    preserveActiveConversationRequestOnNavigation()
    currentConversationIdRef.current = conversationId
    setCurrentConversationId(conversationId)
    setMessages(Array.isArray(conversationMessageCacheRef.current[conversationId]) ? conversationMessageCacheRef.current[conversationId] : [])
    setEditingMessageId(null)
    setComposerAttachments([])
    setChatFeedback('')
    setModelLimitState(null)
    applyConversationState(conversationId)
    navigate(buildChatRoute(conversationId), { replace: isExplicitNewChatView })
    setIsMobileSidebarOpen(false)
  }

  async function loadCaptcha() {
    try {
      const nextCaptcha = await api.getCaptcha()
      setCaptcha(nextCaptcha)
      setAuthForm((prev) => ({ ...prev, captchaAnswer: '' }))
    } catch (error) {
      setAuthError(error instanceof Error ? error.message : '验证码加载失败')
    }
  }

  async function handleAuthSubmit(event: React.FormEvent) {
    event.preventDefault()
    setAuthError('')
    try {
      if (isPasswordResetMode) {
        const identifier = authForm.identifier.trim()
        if (!identifier) {
          setAuthError('请输入邮箱或手机号')
          return
        }
        if (passwordResetStep === 'identity') {
          if (!captcha.captchaId || !authForm.captchaAnswer.trim()) {
            setAuthError('请先完成人机验证码')
            if (!captcha.captchaId) {
              await loadCaptcha()
            }
            return
          }
          const response = await api.requestPasswordReset({
            identifier,
            captchaId: captcha.captchaId,
            captchaAnswer: authForm.captchaAnswer,
          })
          setPasswordResetVerificationState({
            masked: response.identifier,
            kind: response.kind,
            previewCode: response.previewCode,
            deliveryMode: response.deliveryMode,
          })
          setPhoneCodeCooldown(response.expiresInSeconds > 60 ? 60 : response.expiresInSeconds)
          setAuthForm((prev) => ({ ...prev, captchaAnswer: '', verificationCode: '' }))
          setCaptcha({ captchaId: '', imageDataUrl: '', expiresInSeconds: 0 })
          setPasswordResetStep('password')
          await loadCaptcha()
          return
        }
        if (!authForm.verificationCode.trim()) {
          setAuthError('请输入重置验证码')
          return
        }
        if (!authForm.password.trim()) {
          setAuthError('请设置新密码')
          return
        }
        if (authForm.password.length < 8) {
          setAuthError('密码至少需要 8 位')
          return
        }
        if (authForm.password !== authForm.confirmPassword) {
          setAuthError('两次输入的密码不一致')
          return
        }
        if (!captcha.captchaId || !authForm.captchaAnswer.trim()) {
          setAuthError('请先完成人机验证码')
          if (!captcha.captchaId) {
            await loadCaptcha()
          }
          return
        }
        await api.resetPassword({
          identifier,
          verificationCode: authForm.verificationCode,
          captchaId: captcha.captchaId,
          captchaAnswer: authForm.captchaAnswer,
          password: authForm.password,
        })
        setPasswordResetStep('success')
        setCaptcha({ captchaId: '', imageDataUrl: '', expiresInSeconds: 0 })
        return
      }
      if (isLoginMode) {
        if (!captcha.captchaId || !authForm.captchaAnswer.trim()) {
          setAuthError('请先完成人机验证码')
          if (!captcha.captchaId) {
            await loadCaptcha()
          }
          return
        }
        await api.login({ identifier: authForm.identifier, password: authForm.password, captchaId: captcha.captchaId, captchaAnswer: authForm.captchaAnswer })
        resetAuthFlow('login')
        setShowLoginModal(false)
        await bootstrap()
        return
      }

      if (registerStep === 'identity') {
        if (!authForm.displayName.trim()) {
          setAuthError('请先输入昵称')
          return
        }
        if (!normalizedRegisterIdentifier) {
          setAuthError('请先输入邮箱或手机号')
          return
        }
        setRegisterVerificationState(null)
        setPhoneCodeCooldown(0)
        setRegisterStep('verify')
        return
      }

      if (registerStep === 'verify') {
        if (!normalizedRegisterIdentifier) {
          setAuthError('请先输入邮箱或手机号')
          return
        }
        if (!authForm.verificationCode.trim()) {
          setAuthError(`请输入${registerIdentifierLabel}验证码`)
          return
        }
        setRegisterStep('password')
        return
      }

      if (registerStep === 'password') {
        if (!authForm.password.trim()) {
          setAuthError('请先创建密码')
          return
        }
        if (authForm.password.length < 8) {
          setAuthError('密码至少需要 8 位')
          return
        }
        if (!authForm.confirmPassword.trim()) {
          setAuthError('请再次输入密码')
          return
        }
        if (authForm.password !== authForm.confirmPassword) {
          setAuthError('两次输入的密码不一致')
          return
        }
        if (registerRequiresCaptcha) {
          setRegisterStep('captcha')
          return
        }
      }

      if (registerRequiresCaptcha) {
        if (!captcha.captchaId) {
          await loadCaptcha()
          setAuthError('图形验证码已刷新，请重新输入')
          return
        }
        if (!authForm.captchaAnswer.trim()) {
          setAuthError('请输入图形验证码')
          return
        }
      } else {
        const aff = new URLSearchParams(location.search).get('aff') || undefined
        await api.register({
          identifier: normalizedRegisterIdentifier,
          email: registerIdentifierIsEmail ? normalizedRegisterIdentifier : '',
          phone: registerIdentifierIsEmail ? '' : normalizedRegisterIdentifier,
          verificationCode: authForm.verificationCode,
          captchaId: captcha.captchaId,
          captchaAnswer: authForm.captchaAnswer,
          password: authForm.password,
          displayName: authForm.displayName.trim(),
          affiliateCode: aff,
        })
        setCaptcha({ captchaId: '', imageDataUrl: '', expiresInSeconds: 0 })
        setPhoneCodeCooldown(0)
        setRegisterStep('success')
        await bootstrap()
        return
      }

      const aff = new URLSearchParams(location.search).get('aff') || undefined
      await api.register({
        identifier: normalizedRegisterIdentifier,
        email: registerIdentifierIsEmail ? normalizedRegisterIdentifier : '',
        phone: registerIdentifierIsEmail ? '' : normalizedRegisterIdentifier,
        verificationCode: authForm.verificationCode,
        captchaId: captcha.captchaId,
        captchaAnswer: authForm.captchaAnswer,
        password: authForm.password,
        displayName: authForm.displayName.trim(),
        affiliateCode: aff,
      })
      setCaptcha({ captchaId: '', imageDataUrl: '', expiresInSeconds: 0 })
      setPhoneCodeCooldown(0)
      setRegisterStep('success')
      await bootstrap()
    } catch (error) {
      setAuthError(error instanceof Error ? error.message : '请求失败')
      if ((isLoginMode || isPasswordResetMode || (!isLoginMode && registerRequiresCaptcha))) {
        await loadCaptcha()
      }
    }
  }

  async function handleSendRegisterCode() {
    if (!normalizedRegisterIdentifier) {
      setAuthError('请先输入邮箱或手机号')
      return
    }
    setAuthError('')
    setIsSendingVerificationCode(true)
    try {
      const response = await api.sendContactCode({
        identifier: normalizedRegisterIdentifier,
        purpose: 'register',
        captchaId: captcha.captchaId,
        captchaAnswer: authForm.captchaAnswer,
      })
      setRegisterVerificationState({
        masked: response.identifier,
        kind: response.kind,
        previewCode: response.previewCode,
        deliveryMode: response.deliveryMode,
      })
      setPhoneCodeCooldown(response.expiresInSeconds > 60 ? 60 : response.expiresInSeconds)
    } catch (error) {
      setAuthError(error instanceof Error ? error.message : '验证码发送失败')
    } finally {
      setIsSendingVerificationCode(false)
    }
  }

  function handleStartUsing() {
    setShowLoginModal(false)
    resetAuthFlow('login')
  }

  function startPasswordReset() {
    setAuthError('')
    setAuthForm(createEmptyAuthForm())
    setCaptcha({ captchaId: '', imageDataUrl: '', expiresInSeconds: 0 })
    setPhoneCodeCooldown(0)
    setPasswordResetStep('identity')
    setPasswordResetVerificationState(null)
    setIsPasswordResetMode(true)
    setIsLoginMode(true)
    void loadCaptcha()
  }

  function returnToLoginFromPasswordReset() {
    resetAuthFlow('login')
    void loadCaptcha()
  }

  function handleRegisterBack() {
    setAuthError('')
    if (registerStep === 'verify') {
      setRegisterStep('identity')
      return
    }
    if (registerStep === 'password') {
      setRegisterStep('verify')
      return
    }
    if (registerStep === 'captcha') {
      setRegisterStep('password')
    }
  }

  async function handleLogout() {
    cancelActiveRequest()
    conversationMessageCacheRef.current = {}
    await api.logout()
    setCurrentConversationId(null)
    setConversations([])
    setMessages([])
    setModelLimitState(null)
    navigate('/?new=1', { replace: true })
    await bootstrap()
  }

  function requestDeleteConversation(conversationId: string) {
    const target = conversations.find((item) => item.id === conversationId)
    if (!target) {
      return
    }
    setPendingDeleteConversation({
      id: conversationId,
      title: target.title || '这条聊天',
    })
  }

  async function handleDeleteConversation() {
    const conversationId = pendingDeleteConversation?.id
    if (!conversationId) {
      return
    }
    setDeletingConversationId(conversationId)
    try {
      if (hasActiveRequestForConversation(conversationId)) {
        cancelConversationRequest(conversationId)
      }
      await api.deleteConversation(conversationId)
      delete conversationMessageCacheRef.current[conversationId]
      const remaining = conversations.filter((item) => item.id !== conversationId)
      setConversations(remaining)
      if (currentConversationId === conversationId) {
        const nextConversationId = remaining[0]?.id ?? null
        setCurrentConversationId(nextConversationId)
        if (!nextConversationId) {
          setMessages([])
          applyConversationState(null, remaining)
          navigate(buildChatRoute(null), { replace: true })
        } else {
          applyConversationState(nextConversationId, remaining)
          navigate(buildChatRoute(nextConversationId), { replace: true })
        }
        setEditingMessageId(null)
        setInputMessage('')
        setComposerAttachments([])
        setChatFeedback('')
        setModelLimitState(null)
      }
    } catch (error) {
      setChatFeedback(error instanceof Error ? error.message : '删除聊天失败')
    } finally {
      setDeletingConversationId(null)
      setPendingDeleteConversation(null)
    }
  }

  async function persistUserSettings(overrides: Partial<{ theme: string; language: string; deepSearchDefault: boolean; selectedModelSlug: string; chatHistoryEnabled: boolean; memoryEnabled: boolean }> = {}) {
    if (!session?.user) {
      return
    }
    await api.updateUserSettings({
      theme: overrides.theme ?? theme,
      language: overrides.language ?? language,
      deepSearchDefault: overrides.deepSearchDefault ?? isDeepSearch,
      selectedModelSlug: overrides.selectedModelSlug ?? selectedModel?.slug ?? '',
      chatHistoryEnabled: overrides.chatHistoryEnabled ?? chatHistoryEnabled,
      memoryEnabled: overrides.memoryEnabled ?? memoryEnabled,
    })
  }

  function handleSelectModel(option: ModelOption) {
    setSelectedModel(option)
    setIsModelSelectorOpen(false)
    setModelLimitState(null)
    storeSelectedModelSlug(option.slug)
    if (session?.user) {
      void persistUserSettings({ selectedModelSlug: option.slug }).catch(() => undefined)
    }
  }

  async function handleSendMessage() {
    const readyAttachmentIds = composerAttachments.filter((item) => item.status === 'ready' && item.id).map((item) => item.id as string)
    if ((!inputMessage.trim() && readyAttachmentIds.length === 0) || isUploadingAttachment || isTyping) return
    if (!session?.user) {
      setAuthError('')
      setIsLoginMode(true)
      setShowLoginModal(true)
      return
    }
    const previousMessages = messages
    const nextEditingMessageID = editingMessageId
    const currentDraft = inputMessage
    const hasReadyImageAttachment = composerAttachments.some((item) => item.status === 'ready' && item.id && isImageMimeType(item.mimeType))
    const shouldGenerateImage = selectedModel?.slug === 'infinite-ai-photo' || isImageGenerationIntent(currentDraft, hasReadyImageAttachment)
    const effectiveDeepSearch = !shouldGenerateImage && Boolean(isDeepSearch)
    const requestModelSlug = shouldGenerateImage ? 'infinite-ai-photo' : effectiveDeepSearch ? 'infinite-ai-pro' : selectedModel?.slug ?? FALLBACK_MODEL_OPTIONS[0].slug
    const optimisticMessageAssets = mapComposerAttachmentsToMessageAssets(composerAttachments)
    const optimisticUserMessage = createOptimisticUserMessage(currentDraft, requestModelSlug, optimisticMessageAssets)
    const useTemporaryChat = !chatHistoryEnabled
    const editTargetIndex = nextEditingMessageID ? previousMessages.findIndex((item) => item.id === nextEditingMessageID) : -1
    const temporaryHistoryMessages = editTargetIndex >= 0 ? previousMessages.slice(0, editTargetIndex) : previousMessages
    const activeRequest = startActiveRequest(currentConversationId, { isDeepSearch: effectiveDeepSearch })
    const activeRequestId = activeRequest.id
    const activeRequestSignal = activeRequest.controller.signal
    let requestConversationId = currentConversationId
    try {
      if (nextEditingMessageID) {
        setMessages(useTemporaryChat ? temporaryHistoryMessages : trimMessagesAfter(previousMessages, nextEditingMessageID))
        setEditingMessageId(null)
      }
      setChatFeedback('')
      setModelLimitState(null)
      setIsTyping(true)
      setIsDeepSearchThinking(effectiveDeepSearch)
      setInputMessage('')
      setComposerAttachments([])
      if (useTemporaryChat) {
        if (shouldGenerateImage) {
          const optimisticAssistantMessage = createOptimisticImagePlaceholderMessage('infinite-ai-photo')
          setMessages([
            ...temporaryHistoryMessages,
            optimisticUserMessage,
            optimisticAssistantMessage,
          ])
          const response = await api.generateTemporaryChatImage({
            history: buildTemporaryHistoryPayload(temporaryHistoryMessages),
            prompt: currentDraft,
            attachmentIds: readyAttachmentIds,
          }, { signal: activeRequestSignal })
          if (!isActiveRequest(activeRequestId)) {
            return
          }
          if (isViewingConversation(null)) {
            setMessages([
              ...temporaryHistoryMessages,
              response.userMessage,
              response.assistantMessage,
            ])
          }
        } else {
          setMessages([
            ...temporaryHistoryMessages,
            optimisticUserMessage,
          ])
          const response = await api.sendTemporaryMessageStream({
            history: buildTemporaryHistoryPayload(temporaryHistoryMessages),
            content: currentDraft,
            modelSlug: requestModelSlug,
            deepSearch: effectiveDeepSearch,
            attachmentIds: readyAttachmentIds,
          }, { signal: activeRequestSignal })
          if (!response.ok) {
            const payload = await response.json().catch(() => ({ message: response.statusText }))
            const nextModelLimitState = normalizeModelLimitState(payload)
            if (nextModelLimitState) {
              setModelLimitState(nextModelLimitState)
              setIsDeepSearchThinking(false)
              if (nextEditingMessageID) {
                setEditingMessageId(nextEditingMessageID)
              }
              return
            }
            throw new Error(toUserFacingChatErrorMessage(payload.message || payload.error || response.statusText))
          }
          await consumeChatStream(response, null, activeRequestId)
        }
      } else {
        let conversationId = currentConversationId
        if (!conversationId) {
          const created = await api.createConversation({
            title: '新聊天',
            modelSlug: requestModelSlug,
            deepSearch: effectiveDeepSearch,
          }, { signal: activeRequestSignal })
          if (!isActiveRequest(activeRequestId)) {
            return
          }
          conversationId = created.id
          requestConversationId = created.id
          updateActiveRequest(activeRequestId, { conversationId: created.id })
          currentConversationIdRef.current = created.id
          setCurrentConversationId(created.id)
          setConversations((prev) => [created, ...prev])
          navigate(buildChatRoute(created.id), { replace: true })
        }
        const ensuredConversationId = conversationId
        if (!ensuredConversationId) {
          throw new Error('聊天创建失败，请刷新后重试')
        }
        requestConversationId = ensuredConversationId
        if (shouldGenerateImage) {
          applyConversationTitleLocally(ensuredConversationId, currentDraft)
          const optimisticAssistantMessage = createOptimisticImagePlaceholderMessage('infinite-ai-photo')
          applyMessagesForConversation(ensuredConversationId, (prev) => [
            ...prev.filter((item) => !item?.optimistic),
            optimisticUserMessage,
            optimisticAssistantMessage,
          ])
          const response = await api.generateChatImageStream({
            conversationId: ensuredConversationId,
            prompt: currentDraft,
            attachmentIds: readyAttachmentIds,
            editMessageId: nextEditingMessageID ?? undefined,
          }, { signal: activeRequestSignal })
          if (!response.ok) {
            const payload = await response.json().catch(() => ({ message: response.statusText }))
            const nextModelLimitState = normalizeModelLimitState(payload)
            if (nextModelLimitState) {
              setModelLimitState(nextModelLimitState)
              if (nextEditingMessageID) {
                setEditingMessageId(nextEditingMessageID)
              }
              return
            }
            throw new Error(toUserFacingChatErrorMessage(payload.message || payload.error || response.statusText))
          }
          const terminal = await consumeChatStream(response, ensuredConversationId, activeRequestId)
          const activeAfterStream = activeRequestsRef.current[activeRequestId]
          if (!terminal && activeAfterStream?.runId) {
            await subscribeToRunEvents(activeAfterStream.runId, ensuredConversationId, activeRequestId, Number(activeAfterStream.lastSeq ?? 0) || 0)
          }
        } else {
          applyConversationTitleLocally(ensuredConversationId, currentDraft)
          applyMessagesForConversation(ensuredConversationId, (prev) => [
            ...prev.filter((item) => !item?.optimistic),
            optimisticUserMessage,
          ])
          const response = await api.sendMessageStream(ensuredConversationId, {
            content: currentDraft,
            modelSlug: requestModelSlug,
            deepSearch: effectiveDeepSearch,
            attachmentIds: readyAttachmentIds,
            editMessageId: nextEditingMessageID ?? undefined,
          }, { signal: activeRequestSignal })
          if (!response.ok) {
            const payload = await response.json().catch(() => ({ message: response.statusText }))
            const nextModelLimitState = normalizeModelLimitState(payload)
            if (nextModelLimitState) {
              setModelLimitState(nextModelLimitState)
              setIsDeepSearchThinking(false)
              if (nextEditingMessageID) {
                setEditingMessageId(nextEditingMessageID)
              }
              return
            }
            throw new Error(toUserFacingChatErrorMessage(payload.message || payload.error || response.statusText))
          }
          const terminal = await consumeChatStream(response, ensuredConversationId, activeRequestId)
          const activeAfterStream = activeRequestsRef.current[activeRequestId]
          if (!terminal && activeAfterStream?.runId) {
            await subscribeToRunEvents(activeAfterStream.runId, ensuredConversationId, activeRequestId, Number(activeAfterStream.lastSeq ?? 0) || 0)
          }
        }
      }
      if (!isActiveRequest(activeRequestId)) {
        return
      }
      if (isViewingConversation(requestConversationId)) {
        setInputMessage('')
        setComposerAttachments([])
        setEditingMessageId(null)
        setModelLimitState(null)
      }
      if (!useTemporaryChat) {
        const refreshed = await api.listConversations()
        if (!isActiveRequest(activeRequestId)) {
          return
        }
        setConversations(Array.isArray(refreshed.conversations) ? refreshed.conversations : [])
      }
    } catch (error) {
      if (isAbortError(error)) {
        return
      }
      const nextModelLimitState = normalizeModelLimitState((error as { payload?: unknown } | null)?.payload)
      if (nextModelLimitState) {
        if (isViewingConversation(requestConversationId)) {
          setModelLimitState(nextModelLimitState)
          setIsDeepSearchThinking(false)
        } else if (requestConversationId) {
          applyMessagesForConversation(requestConversationId, (prev) => prev)
        }
        if (nextEditingMessageID) {
          setEditingMessageId(nextEditingMessageID)
        }
        return
      }
      const message = toUserFacingChatErrorMessage(error instanceof Error ? error.message : error)
      const assistantNotice = createLocalAssistantNoticeMessage(message, requestModelSlug)
      if (isViewingConversation(requestConversationId)) {
        setIsDeepSearchThinking(false)
        if (nextEditingMessageID) {
          setEditingMessageId(nextEditingMessageID)
          setMessages((prev) => [...prev, assistantNotice])
          return
        }
        setChatFeedback('')
        setMessages((prev) => {
          const hasNotice = prev.some((item) => item.id === assistantNotice.id)
          if (hasNotice) {
            return prev
          }
          return [...prev, assistantNotice]
        })
      } else if (requestConversationId) {
        applyMessagesForConversation(requestConversationId, (prev) => [...prev, assistantNotice])
      }
    } finally {
      finishActiveRequest(activeRequestId)
    }
  }

  async function subscribeToRunEvents(runId: string, conversationId: string, requestId: number, afterSeq = 0) {
    let nextAfterSeq = afterSeq
    let reconnectFailures = 0
    while (isActiveRequest(requestId)) {
      try {
        const response = await api.streamChatRunEvents(runId, nextAfterSeq, {
          signal: activeRequestsRef.current[requestId]?.controller.signal,
        })
        if (!response.ok) {
          const payload = await response.json().catch(() => ({ message: response.statusText }))
          throw new Error(toUserFacingChatErrorMessage(payload.message || payload.error || '聊天续接失败', '聊天续接失败，请刷新后重试'))
        }
        const terminal = await consumeChatStream(response, conversationId, requestId, runId)
        reconnectFailures = 0
        nextAfterSeq = Number(activeRequestsRef.current[requestId]?.lastSeq ?? nextAfterSeq) || nextAfterSeq
        if (terminal || !isActiveRequest(requestId)) {
          if (terminal) {
            finishActiveRequest(requestId)
          }
          return
        }
        await new Promise<void>((resolve) => window.setTimeout(resolve, 600))
      } catch (error) {
        if (isAbortError(error) || !isActiveRequest(requestId)) {
          return
        }
        reconnectFailures += 1
        const settled = await settleRunFromServer(runId, conversationId, requestId)
        if (settled || !isActiveRequest(requestId)) {
          return
        }
        try {
          const activeRuns = await api.listActiveChatRuns(conversationId)
          const stillRunning = (Array.isArray(activeRuns.runs) ? activeRuns.runs : []).some((run: any) => String(run?.id ?? '') === runId)
          if (!stillRunning) {
            await refreshConversationMessagesFromServer(conversationId)
            finishActiveRequest(requestId)
            return
          }
        } catch {
          // 续接对账失败时先重试，避免长图生成在网关断开后误报失败。
        }
        if (reconnectFailures <= 5) {
          await new Promise<void>((resolve) => window.setTimeout(resolve, 1200))
          continue
        }
        const notice = createLocalAssistantNoticeMessage('聊天续接失败，请刷新后重试。', selectedModel?.slug ?? FALLBACK_MODEL_OPTIONS[0].slug)
        applyMessagesForConversation(conversationId, (prev) => [...prev, notice])
        finishActiveRequest(requestId)
        return
      }
    }
  }

  async function consumeChatStream(response: Response, conversationId?: string | null, requestId?: number, runIdHint?: string) {
    const reader = response.body?.getReader()
    if (!reader) {
      throw new Error('流式连接不可用，请刷新后重试')
    }
    const decoder = new TextDecoder()
    const streamScope = conversationId ?? 'temporary'
    let streamingMessageId = runIdHint ? `stream-${runIdHint}` : `stream-${streamScope}-${requestId ?? Date.now()}`
    let buffer = ''
    let sawTerminalEvent = false
    while (true) {
      if (requestId && !isActiveRequest(requestId)) {
        await reader.cancel().catch(() => undefined)
        return
      }
      const { value, done } = await reader.read()
      if (done) break
      if (requestId && !isActiveRequest(requestId)) {
        await reader.cancel().catch(() => undefined)
        return
      }
      buffer += decoder.decode(value, { stream: true })
      let boundary = buffer.indexOf('\n\n')
      while (boundary >= 0) {
        const rawEvent = buffer.slice(0, boundary).trim()
        buffer = buffer.slice(boundary + 2)
        const data = rawEvent
          .split('\n')
          .filter((line) => line.startsWith('data: '))
          .map((line) => line.slice(6))
          .join('\n')
        if (data) {
          const payload = JSON.parse(data)
          if (typeof payload.seq === 'number' && requestId) {
            updateActiveRequest(requestId, { lastSeq: payload.seq })
          }
          if (payload.type === 'run_started' && payload.runId) {
            const nextRunId = String(payload.runId)
            streamingMessageId = `stream-${nextRunId}`
            if (requestId) {
              updateActiveRequest(requestId, { runId: nextRunId })
            }
          }
          if (requestId && !isActiveRequest(requestId)) {
            return
          }
          if (payload.type === 'search_sources') {
            upsertStreamingAssistantSources(streamingMessageId, Array.isArray(payload.sources) ? payload.sources : [], conversationId)
          }
          if (payload.type === 'image_pending') {
            const pendingMessage = createOptimisticImagePlaceholderMessage(
              String(payload.model || selectedModel?.slug || 'infinite-ai-photo'),
              streamingMessageId,
              false,
              String(payload.message || '正在生成照片'),
            )
            const updateMessages = (prev: any[]) => {
              const withoutOldPending = prev.filter((item) => item.id === streamingMessageId || !isPendingImagePlaceholder(item))
              const exists = withoutOldPending.some((item) => item.id === streamingMessageId)
              if (!exists) {
                return [...withoutOldPending, pendingMessage]
              }
              return withoutOldPending.map((item) => (item.id === streamingMessageId ? { ...pendingMessage, createdAt: item.createdAt ?? pendingMessage.createdAt } : item))
            }
            if (conversationId) {
              applyMessagesForConversation(conversationId, updateMessages)
            } else if (isViewingConversation(null)) {
              setMessages(updateMessages)
            }
          }
          if (payload.type === 'user' && payload.message) {
            if (conversationId) {
              applyMessagesForConversation(conversationId, (prev) => [
                ...prev.filter((item) => item.id !== payload.message.id && !(item?.optimistic && item.role === 'user')),
                payload.message,
              ])
            } else if (isViewingConversation(null)) {
              setMessages((prev) => [
                ...prev.filter((item) => item.id !== payload.message.id && !(item?.optimistic && item.role === 'user')),
                payload.message,
              ])
            }
          }
          if (payload.type === 'assistant_reasoning') {
            if (requestId) {
              updateActiveRequest(requestId, { phase: 'answering' })
            }
            if (isViewingConversation(conversationId ?? null)) {
              setIsDeepSearchThinking(false)
              openReasoningPanel(streamingMessageId)
            }
            await animateStreamingText(streamingMessageId, 'reasoningContent', String(payload.reasoning ?? ''), requestId, conversationId)
          }
          if (payload.type === 'assistant_delta') {
            if (requestId) {
              updateActiveRequest(requestId, { phase: 'answering' })
            }
            if (isViewingConversation(conversationId ?? null)) {
              setIsDeepSearchThinking(false)
            }
            await animateStreamingText(streamingMessageId, 'content', String(payload.delta ?? ''), requestId, conversationId)
          }
          if (payload.type === 'done' && payload.assistantMessage) {
            sawTerminalEvent = true
            if (requestId) {
              updateActiveRequest(requestId, { phase: 'answering' })
            }
            if (isViewingConversation(conversationId ?? null)) {
              setIsDeepSearchThinking(false)
            }
            if (conversationId) {
              applyMessagesForConversation(conversationId, (prev) => {
                const shouldRemovePendingImage = isImageAssistantMessage(payload.assistantMessage)
                return mergeTerminalAssistantMessage(prev, streamingMessageId, payload.assistantMessage, shouldRemovePendingImage)
              })
            } else if (isViewingConversation(null)) {
              setMessages((prev) => {
                const shouldRemovePendingImage = isImageAssistantMessage(payload.assistantMessage)
                return mergeTerminalAssistantMessage(prev, streamingMessageId, payload.assistantMessage, shouldRemovePendingImage)
              })
            }
            if (isViewingConversation(conversationId ?? null) && payload.assistantMessage?.reasoningContent) {
              openReasoningPanel(payload.assistantMessage.id ?? streamingMessageId)
            }
            if (conversationId && payload.title) {
              applyConversationTitleLocally(conversationId, String(payload.title), true)
            }
          }
          if (payload.type === 'canceled') {
            sawTerminalEvent = true
            if (payload.assistantMessage) {
              if (conversationId) {
                applyMessagesForConversation(conversationId, (prev) => {
                  return mergeTerminalAssistantMessage(prev, streamingMessageId, payload.assistantMessage)
                })
              } else if (isViewingConversation(null)) {
                setMessages((prev) => mergeTerminalAssistantMessage(prev, streamingMessageId, payload.assistantMessage))
              }
            }
            if (requestId) {
              finishActiveRequest(requestId)
            }
          }
          if (payload.type === 'error') {
            if (payload.assistantMessage) {
              if (conversationId) {
                applyMessagesForConversation(conversationId, (prev) => {
                  return mergeTerminalAssistantMessage(prev, streamingMessageId, payload.assistantMessage, true)
                })
              } else if (isViewingConversation(null)) {
                setMessages((prev) => mergeTerminalAssistantMessage(prev, streamingMessageId, payload.assistantMessage, true))
              }
              if (requestId) {
                finishActiveRequest(requestId)
              }
              return true
            }
            throw new Error(toUserFacingChatErrorMessage(payload.message))
          }
        }
        boundary = buffer.indexOf('\n\n')
      }
    }
    return sawTerminalEvent
  }

  function getFilesFromDataTransfer(dataTransfer: DataTransfer | null) {
    if (!dataTransfer) {
      return []
    }
    const files = Array.from(dataTransfer.files ?? []).filter(Boolean)
    if (files.length > 0) {
      return files
    }
    return Array.from(dataTransfer.items ?? [])
      .filter((item) => item.kind === 'file')
      .map((item) => item.getAsFile())
      .filter((file): file is File => Boolean(file))
  }

  function hasFileDataTransfer(dataTransfer: DataTransfer | null) {
    if (!dataTransfer) {
      return false
    }
    if (Array.from(dataTransfer.types ?? []).includes('Files')) {
      return true
    }
    return Array.from(dataTransfer.items ?? []).some((item) => item.kind === 'file')
  }

  function handleComposerDragEnter(event: DragEvent<HTMLDivElement>) {
    if (!hasFileDataTransfer(event.dataTransfer)) {
      return
    }
    event.preventDefault()
    event.stopPropagation()
    setIsComposerDragActive(true)
  }

  function handleComposerDragOver(event: DragEvent<HTMLDivElement>) {
    if (!hasFileDataTransfer(event.dataTransfer)) {
      return
    }
    event.preventDefault()
    event.stopPropagation()
    event.dataTransfer.dropEffect = 'copy'
    setIsComposerDragActive(true)
  }

  function handleComposerDragLeave(event: DragEvent<HTMLDivElement>) {
    event.preventDefault()
    event.stopPropagation()
    const nextTarget = event.relatedTarget
    if (nextTarget instanceof Node && event.currentTarget.contains(nextTarget)) {
      return
    }
    setIsComposerDragActive(false)
  }

  function handleComposerDrop(event: DragEvent<HTMLDivElement>) {
    const files = getFilesFromDataTransfer(event.dataTransfer)
    if (files.length === 0) {
      return
    }
    event.preventDefault()
    event.stopPropagation()
    setIsComposerDragActive(false)
    void handleAttachmentFiles(files)
    window.requestAnimationFrame(() => composerTextareaRef.current?.focus())
  }

  function handleComposerPaste(event: ClipboardEvent<HTMLTextAreaElement>) {
    const files = getFilesFromDataTransfer(event.clipboardData)
    if (files.length === 0) {
      return
    }
    event.preventDefault()
    void handleAttachmentFiles(files)
    window.requestAnimationFrame(() => composerTextareaRef.current?.focus())
  }

  async function handleAttachmentFiles(fileList: FileList | File[] | null) {
    if (!fileList?.length) return
    if (!session?.user) {
      setIsLoginMode(true)
      setShowLoginModal(true)
      return
    }
    const files = Array.from(fileList)
    for (const file of files) {
      const clientId = `${Date.now()}-${file.name}-${Math.random().toString(36).slice(2, 8)}`
      const mimeType = file.type || 'application/octet-stream'
      const previewUrl = isImageMimeType(mimeType) ? URL.createObjectURL(file) : undefined
      setComposerAttachments((prev) => [
        ...prev,
        {
          clientId,
          fileName: file.name,
          mimeType,
          sizeBytes: file.size,
          previewUrl,
          status: 'uploading',
        },
      ])
      try {
        const init = await api.initAttachmentUpload({
          conversationId: currentConversationId ?? undefined,
          fileName: file.name,
          mimeType,
          sizeBytes: file.size,
        })
        await api.uploadAttachmentBinary(init.upload.url, file)
        const completed = await api.completeAttachment(init.attachment.id)
        setComposerAttachments((prev) =>
          prev.map((item) =>
            item.clientId === clientId
              ? {
                  ...item,
                  id: completed.attachment.id,
                  fileName: completed.attachment.fileName,
                  mimeType: completed.attachment.mimeType,
                  sizeBytes: file.size,
                  status: 'ready',
                  error: '',
                }
              : item,
          ),
        )
      } catch (error) {
        setComposerAttachments((prev) =>
          prev.map((item) =>
            item.clientId === clientId
              ? {
                  ...item,
                  status: 'error',
                  error: error instanceof Error ? error.message : '上传失败',
                }
              : item,
          ),
        )
      }
    }
    if (fileInputRef.current) fileInputRef.current.value = ''
    if (imageInputRef.current) imageInputRef.current.value = ''
  }

  function removeComposerAttachment(clientId: string) {
    setComposerAttachments((prev) => prev.filter((item) => item.clientId !== clientId))
  }

  function handleEditMessage(message: any) {
    setEditingMessageId(message.id)
    setInputMessage(message.content ?? '')
    setComposerAttachments(mapMessageAttachmentsToComposer(message))
    setChatFeedback('')
    window.requestAnimationFrame(() => composerTextareaRef.current?.focus())
  }

  function cancelEditingMessage() {
    setEditingMessageId(null)
    setChatFeedback('')
    setInputMessage('')
    setComposerAttachments([])
  }

  async function handleCopy(text: string, id: string) {
    let copied = copyTextWithTextareaFallback(text)
    if (!copied && window.isSecureContext && navigator.clipboard?.writeText) {
      try {
        await navigator.clipboard.writeText(text)
        copied = true
      } catch {
        copied = false
      }
    }
    if (copied) {
      setCopiedStates((prev) => ({ ...prev, [id]: true }))
      window.setTimeout(() => {
        setCopiedStates((prev) => ({ ...prev, [id]: false }))
      }, 2000)
    } else {
      setCopiedStates((prev) => ({ ...prev, [id]: false }))
    }
  }

  async function handleShareMessage(
    text: string,
    id: string,
    options?: {
      title?: string
      url?: string
      onFallbackCopied?: () => void
    },
  ) {
    const normalizedText = normalizeVisibleMessageContent(text)
    const normalizedURL = String(options?.url ?? '').trim()
    if (!normalizedText && !normalizedURL) {
      return
    }
    if (navigator.share) {
      try {
        await navigator.share({
          title: options?.title,
          text: normalizedText || undefined,
          url: normalizedURL || undefined,
        })
        return
      } catch (error) {
        if (error instanceof DOMException && error.name === 'AbortError') {
          return
        }
      }
    }
    await handleCopy(normalizedURL || normalizedText, id)
    options?.onFallbackCopied?.()
  }

  async function handleShareConversationMessage(message: any) {
    const normalizedText = normalizeVisibleMessageContent(message?.content)
    if (!normalizedText) {
      return
    }
    const shareURL = await ensureConversationShareLink()
    if (shareURL) {
      await handleShareMessage(normalizedText, `assistant-share-${message.id}`, {
        title: currentConversationTitle,
        url: buildMessageAnchorShareURL(shareURL, String(message?.id ?? '')),
        onFallbackCopied: () => setChatFeedback('这条回答的分享链接已复制。'),
      })
      return
    }
    await handleShareMessage(normalizedText, `assistant-share-${message.id}`, {
      title: currentConversationTitle,
    })
  }

  async function handleShareSharedConversationMessage(message: any) {
    const normalizedText = normalizeVisibleMessageContent(message?.content)
    if (!normalizedText) {
      return
    }
    const currentURL = typeof window !== 'undefined' ? window.location.href : ''
    await handleShareMessage(normalizedText, `assistant-share-${message.id}`, {
      title: sharedConversation?.title || '对话分享',
      url: buildMessageAnchorShareURL(currentURL, String(message?.id ?? '')),
      onFallbackCopied: () => setSharedChatFeedback('这条回答的分享链接已复制。'),
    })
  }

  function copyTextWithTextareaFallback(text: string) {
    if (typeof document === 'undefined') {
      return false
    }
    const textarea = document.createElement('textarea')
    textarea.value = text
    textarea.setAttribute('readonly', 'true')
    textarea.style.position = 'fixed'
    textarea.style.left = '-9999px'
    textarea.style.top = '0'
    document.body.appendChild(textarea)
    textarea.focus()
    textarea.select()
    try {
      return document.execCommand('copy')
    } catch {
      return false
    } finally {
      document.body.removeChild(textarea)
    }
  }

  function mergeTerminalAssistantMessage(prev: any[], streamingId: string, assistantMessage: any, removePendingImage = false) {
    const scopedPrev = removePendingImage
      ? prev.filter((item) => item.id === streamingId || !isPendingImagePlaceholder(item))
      : prev
    const assistantId = String(assistantMessage?.id ?? '').trim()
    const hasPersistedAssistant = assistantId ? scopedPrev.some((item) => item.id === assistantId) : false
    const hasStreamingPlaceholder = scopedPrev.some((item) => item.id === streamingId)
    if (hasStreamingPlaceholder) {
      return scopedPrev.map((item) => (item.id === streamingId ? assistantMessage : item))
    }
    if (hasPersistedAssistant) {
      return scopedPrev.map((item) => (item.id === assistantId ? assistantMessage : item))
    }
    return [...scopedPrev, assistantMessage]
  }

  async function openArtifact(id: string) {
    if (!id) return
    setArtifactStatus('正在加载代码预览...')
    try {
      const response = await api.getChatArtifact(id)
      const artifact = response.artifact ?? response
      const files = Array.isArray(artifact.files) ? artifact.files : []
      setActiveArtifact({ ...artifact, versions: response.versions ?? [] })
      setArtifactDraftFiles(files)
      setActiveArtifactFilePath(artifact.entryFile || files[0]?.path || '')
      setArtifactStatus('')
    } catch (error) {
      setArtifactStatus(error instanceof Error ? error.message : '代码预览加载失败')
    }
  }

  function updateArtifactFile(path: string, content: string) {
    setArtifactDraftFiles((prev) => prev.map((file) => (file.path === path ? { ...file, content } : file)))
  }

  async function saveArtifactVersion() {
    if (!activeArtifact?.id) return
    setArtifactStatus('正在保存新版本...')
    try {
      const response = await api.saveChatArtifactVersion(activeArtifact.id, artifactDraftFiles)
      setActiveArtifact((prev: any) => ({
        ...prev,
        version: response.version?.version ?? prev?.version,
        versions: [response.version, ...(Array.isArray(prev?.versions) ? prev.versions : [])].filter(Boolean),
      }))
      setArtifactStatus('新版本已保存')
    } catch (error) {
      setArtifactStatus(error instanceof Error ? error.message : '代码版本保存失败')
    }
  }

  async function handleCreateApiKey() {
    try {
      const created = await api.createApiKey({
        name: `项目密钥 ${apiKeys.length + 1}`,
        scopes: ['chat:write', 'images:generate'],
        rateLimitPerMinute: 120,
      })
      setApiKeys((prev) => [created, ...prev])
      if (created.revealedKey) {
        handleCopy(created.revealedKey, created.id)
      }
    } catch (error) {
      setPaymentFeedback(error instanceof Error ? error.message : '创建失败')
    }
  }

  async function handlePay() {
    if (!checkoutData) return
    try {
      const payload =
        checkoutData.type === 'plan'
          ? { type: 'plan', planCode: checkoutData.planCode, subMethod: selectedPaymentMethod }
          : {
              type: 'recharge',
              rechargeAmount: Number(checkoutData.amount),
              subMethod: selectedPaymentMethod,
            }
      const response = await api.createOrder(payload)
      setPaymentFeedback(response.payment?.message || response.order?.status || '订单已创建')
    } catch (error) {
      setPaymentFeedback(error instanceof Error ? error.message : '支付创建失败')
    }
  }

  async function handleSaveSettings() {
    const nextChatHistoryEnabled = chatHistoryEnabled
    const nextMemoryEnabled = memoryEnabled
    const modeChanged = nextChatHistoryEnabled !== savedChatHistoryEnabled
    const memoryChanged = nextMemoryEnabled !== savedMemoryEnabled
    await persistUserSettings()
    setSavedChatHistoryEnabled(nextChatHistoryEnabled)
    setSavedMemoryEnabled(nextMemoryEnabled)
    if (modeChanged) {
      setCurrentConversationId(null)
      setConversations([])
      setMessages([])
      setEditingMessageId(null)
      setComposerAttachments([])
      setModelLimitState(null)
      setChatFeedback(
        nextChatHistoryEnabled
          ? '已开启聊天记录保存，新的对话会进入最近聊天。'
          : '已关闭聊天记录保存，新的对话将进入临时聊天模式，不会写入账号历史。',
      )
    } else if (memoryChanged) {
      setChatFeedback(nextMemoryEnabled ? '已开启账号级记忆，同一账号的新对话会参考过往对话。' : '已关闭账号级记忆，新对话只会参考当前对话内容。')
    }
    await bootstrap()
    setShowSettingsModal(false)
  }

  async function handleClearChats() {
    cancelActiveRequest()
    conversationMessageCacheRef.current = {}
    await api.clearChats()
    setConversations([])
    setMessages([])
    setCurrentConversationId(null)
  }

  async function handleExportData() {
    const response = await api.exportData()
    const text = await response.text()
    handleCopy(text, 'export')
  }

  async function handleDeleteAccount() {
    await api.deleteAccount()
    await api.logout()
    await bootstrap()
  }

  function planByCode(code: string) {
    return plans.find((item) => item.code === code)
  }

  function openPlanCheckout(code: string) {
    const plan = planByCode(code)
    if (!plan) return
    setCheckoutData({
      title: plan.name,
      amount: (plan.priceCents / 100).toFixed(2),
      type: 'plan',
      planCode: plan.code,
      desc: '按月订阅',
    })
    setPaymentFeedback('')
    navigateTo('payment')
  }

  const readyAttachmentCount = composerAttachments.filter((item) => item.status === 'ready' && item.id).length
  const isUploadingAttachment = composerAttachments.some((item) => item.status === 'uploading')
  const canSendMessage = Boolean((inputMessage.trim() || readyAttachmentCount > 0) && !isUploadingAttachment && !isTyping)
  const visibleActiveRequest = getActiveRequestForConversation(currentConversationId)
  const showStopButton = Boolean(visibleActiveRequest)
  const showGlobalTypingIndicator = isTyping && !messages.some((message) => isVisibleAssistantProgressMessage(message))

  function renderCaptchaChallenge(refreshLabel = '刷新人机验证码') {
    const challengeType = captcha.challengeType ?? 'text'
    const selectedAnswer = authForm.captchaAnswer.trim()
    return (
      <div className="space-y-3">
        <div className={`rounded-xl border px-3 py-3 ${colors.inputBg} ${colors.border}`}>
          <div className="flex items-center justify-between gap-3">
            {captcha.imageDataUrl ? (
              <img src={captcha.imageDataUrl} alt="人机验证" className={`${challengeType === 'text' ? 'h-12' : 'h-24'} max-w-full rounded-md object-contain`} />
            ) : (
              <div className={`h-12 w-32 rounded-md border ${colors.border}`} />
            )}
            <button type="button" onClick={() => void loadCaptcha()} className={`shrink-0 text-sm ${colors.textMuted} hover:underline`}>
              {refreshLabel}
            </button>
          </div>
          {captcha.prompt && <div className={`mt-3 text-xs leading-5 ${colors.textMuted}`}>{captcha.prompt}</div>}
        </div>
        {challengeType === 'slide' ? (
          <div className={`rounded-xl border px-4 py-4 ${colors.inputBg} ${colors.border}`}>
            <input
              type="range"
              min="0"
              max="100"
              step="1"
              value={Number(authForm.captchaAnswer || 0)}
              onChange={(event) => setAuthForm((prev) => ({ ...prev, captchaAnswer: event.target.value }))}
              className="w-full accent-blue-500"
            />
            <div className={`mt-2 text-center text-xs ${colors.textMuted}`}>当前位置：{authForm.captchaAnswer || 0}%</div>
          </div>
        ) : challengeType === 'choice' ? (
          <div className="grid grid-cols-2 gap-2">
            {(captcha.options ?? []).map((option) => (
              <button
                key={option.value}
                type="button"
                onClick={() => setAuthForm((prev) => ({ ...prev, captchaAnswer: option.value }))}
                className={`rounded-xl border px-3 py-3 text-sm font-medium ${
                  selectedAnswer === option.value
                    ? isDark ? 'border-blue-400 bg-blue-500/15 text-blue-200' : 'border-blue-500 bg-blue-50 text-blue-700'
                    : `${colors.border} ${colors.hover}`
                }`}
              >
                {option.label}
              </button>
            ))}
          </div>
        ) : (
          <input
            value={authForm.captchaAnswer}
            onChange={(event) => setAuthForm((prev) => ({ ...prev, captchaAnswer: event.target.value }))}
            className={`w-full px-4 py-3 rounded-md border ${colors.inputBg} ${colors.border} ${colors.textMain}`}
            placeholder="输入人机验证码"
            required
          />
        )}
      </div>
    )
  }

  function renderRichInlineText(text: string, keyPrefix: string) {
    const nodes: ReactNode[] = []
    const inlineParts = String(text).split(/(`[^`\n]+`)/g)
    inlineParts.forEach((part, inlineIndex) => {
      if (!part) {
        return
      }
      if (part.startsWith('`') && part.endsWith('`')) {
        nodes.push(
          <code key={`${keyPrefix}-code-${inlineIndex}`} className={`rounded-md border px-1.5 py-0.5 font-mono text-[0.92em] ${colors.border} ${isDark ? 'bg-[#171717]' : 'bg-[#f3f4f6]'}`}>
            {part.slice(1, -1)}
          </code>,
        )
        return
      }
      const linkPattern = /\[([^\]]+)\]\((https?:\/\/[^\s)]+)\)|(https?:\/\/[^\s<]+[^\s<.,;:!?，。；：！？）)\]])/g
      let cursor = 0
      let match: RegExpExecArray | null
      while ((match = linkPattern.exec(part))) {
        if (match.index > cursor) {
          nodes.push(part.slice(cursor, match.index))
        }
        const label = match[1] || match[3]
        const href = match[2] || match[3]
        nodes.push(
          <a
            key={`${keyPrefix}-link-${inlineIndex}-${match.index}`}
            href={href}
            target="_blank"
            rel="noreferrer"
            className="text-blue-500 underline decoration-blue-500/40 underline-offset-4 hover:decoration-blue-500"
          >
            {label}
          </a>,
        )
        cursor = match.index + match[0].length
      }
      if (cursor < part.length) {
        nodes.push(part.slice(cursor))
      }
    })
    return nodes
  }

  function renderMessageContent(content: unknown, messageId: string) {
    const segments = splitMessageContent(normalizeVisibleMessageContent(content))
    if (segments.length === 0) {
      return null
    }
    return (
      <div className="space-y-4">
        {segments.map((segment, index) => {
          if (segment.type === 'text') {
            const text = segment.text
            if (!text.trim()) {
              return null
            }
            return (
              <div key={`${messageId}-text-${index}`} className="text-base leading-relaxed break-words whitespace-pre-wrap">
                {renderRichInlineText(text, `${messageId}-text-${index}`)}
              </div>
            )
          }
          const blockId = `${messageId}-code-${index}`
          const languageLabel = segment.language || 'code'
          const canPreview = isPreviewableCode(segment.language, segment.code)
          const previewOpen = Boolean(previewCodeBlocks[blockId])
          return (
            <div key={blockId} className={`overflow-hidden rounded-2xl border ${colors.border} ${isDark ? 'bg-[#101010]' : 'bg-[#f7f7f8]'}`}>
              <div className={`flex items-center justify-between gap-3 border-b px-4 py-2 text-xs ${colors.border} ${colors.textMuted}`}>
                <span className="font-mono uppercase tracking-[0.14em]">{languageLabel}</span>
                <div className="flex items-center gap-2">
                  {canPreview && (
                    <button
                      type="button"
                      onClick={() => setPreviewCodeBlocks((prev) => ({ ...prev, [blockId]: !prev[blockId] }))}
                      className={`rounded-full border px-3 py-1 ${colors.border} ${colors.hover}`}
                    >
                      {previewOpen ? '收起预览' : '预览'}
                    </button>
                  )}
                  <button
                    type="button"
                    onClick={() => handleCopy(segment.code, `copy-${blockId}`)}
                    className={`rounded-full border px-3 py-1 ${colors.border} ${colors.hover}`}
                  >
                    {copiedStates[`copy-${blockId}`] ? '已复制' : '复制代码'}
                  </button>
                </div>
              </div>
              {canPreview && previewOpen ? (
                <div className="p-3">
                  <iframe
                    title={`${languageLabel} 预览`}
                    srcDoc={buildCodePreviewDocument(segment.language, segment.code)}
                    sandbox=""
                    className={`h-[360px] w-full rounded-xl border bg-white ${colors.border}`}
                  />
                </div>
              ) : (
                <pre className={`overflow-x-auto p-4 text-sm leading-6 ${isDark ? 'text-[#e6edf3]' : 'text-[#111827]'}`}>
                  <code className="font-mono">{segment.code}</code>
                </pre>
              )}
            </div>
          )
        })}
      </div>
    )
  }

  function renderReasoningPanel(message: any) {
    const reasoning = typeof message?.reasoningContent === 'string' ? message.reasoningContent.trim() : ''
    if (!reasoning) {
      return null
    }
    const expanded = Boolean(expandedReasoningPanels[message.id])
    return (
      <div className={`mb-3 overflow-hidden rounded-2xl border ${colors.border} ${isDark ? 'bg-[#171717]' : 'bg-[#fafafa]'}`}>
        <button
          type="button"
          onClick={() => setExpandedReasoningPanels((prev) => ({ ...prev, [message.id]: !prev[message.id] }))}
          className={`flex w-full items-center justify-between gap-3 px-4 py-3 text-left ${colors.hover}`}
        >
          <div className="min-w-0">
            <div className="text-sm font-medium">深度搜索思考</div>
            <div className={`mt-1 text-xs ${colors.textMuted}`}>{expanded ? '已展开详细思路摘要' : '点击展开查看这次深度搜索的思路摘要'}</div>
          </div>
          <ChevronDown className={`h-4 w-4 shrink-0 transition-transform ${expanded ? 'rotate-180' : ''}`} />
        </button>
        {expanded && <div className={`border-t px-4 py-4 text-sm leading-7 whitespace-pre-wrap ${colors.border} ${colors.textMuted}`}>{reasoning}</div>}
      </div>
    )
  }

  function renderMessageSources(messageId: string, sources: any[]) {
    if (!Array.isArray(sources) || sources.length === 0) {
      return null
    }
    const expanded = Boolean(expandedSourcePanels[messageId])
    return (
      <div className={`mb-4 overflow-hidden rounded-2xl border ${colors.border} ${isDark ? 'bg-[#171717]' : 'bg-[#fafafa]'}`}>
        <button
          type="button"
          onClick={() => setExpandedSourcePanels((prev) => ({ ...prev, [messageId]: !prev[messageId] }))}
          className={`flex w-full items-center justify-between gap-3 px-4 py-3 text-left ${colors.hover}`}
        >
          <div className="flex min-w-0 items-center gap-2">
            <Globe className="h-4 w-4 shrink-0" />
            <div className="min-w-0">
              <div className="text-sm font-medium">联网检索来源</div>
              <div className={`mt-0.5 text-xs ${colors.textMuted}`}>{sources.length} 个来源，默认收起</div>
            </div>
          </div>
          <div className="flex items-center gap-2 shrink-0">
            <span className={`text-xs ${colors.textMuted}`}>{expanded ? '收起' : '展开'}</span>
            <ChevronDown className={`h-4 w-4 transition-transform ${expanded ? 'rotate-180' : ''}`} />
          </div>
        </button>
        {expanded && (
          <div className={`grid gap-2 border-t p-3 sm:grid-cols-2 ${colors.border}`}>
            {sources.map((source: any, index: number) => {
              const href = String(source?.url ?? '')
              return (
                <a
                  key={`${href}-${index}`}
                  href={href}
                  target="_blank"
                  rel="noreferrer"
                  className={`block rounded-xl border p-3 text-left transition ${colors.border} ${colors.hover}`}
                >
                  <div className="text-xs font-semibold text-blue-500">来源 {source?.index ?? index + 1}</div>
                  <div className="mt-1 line-clamp-2 text-sm font-medium">{source?.title || href}</div>
                  {source?.domain && <div className={`mt-1 truncate text-xs ${colors.textMuted}`}>{source.domain}</div>}
                  {source?.snippet && <div className={`mt-2 line-clamp-3 text-xs leading-5 ${colors.textMuted}`}>{source.snippet}</div>}
                </a>
              )
            })}
          </div>
        )}
      </div>
    )
  }

  function renderMessageArtifacts(artifacts: any[]) {
    if (view === 'shared-chat') {
      return null
    }
    if (!Array.isArray(artifacts) || artifacts.length === 0) {
      return null
    }
    return (
      <div className="mt-4 flex flex-wrap gap-2">
        {artifacts.map((artifact: any) => (
          <button
            key={artifact.id}
            type="button"
            onClick={() => void openArtifact(artifact.id)}
            className={`inline-flex items-center gap-2 rounded-full border px-3 py-1.5 text-sm ${colors.border} ${colors.hover}`}
          >
            <Code2 className="h-4 w-4" />
            打开 {artifact.title || '代码预览'}
          </button>
        ))}
      </div>
    )
  }

  function renderMessageAttachments(attachments: any[]) {
    if (!Array.isArray(attachments) || attachments.length === 0) {
      return null
    }
    return (
      <div className="mt-3 flex flex-wrap gap-3">
        {attachments.map((asset: any) => {
          if (asset?.pending) {
            return (
              <div key={asset.id ?? asset.fileName ?? 'pending-image'} className="flex w-full max-w-[520px] flex-col gap-5">
                <div className="space-y-2">
                  <div className={`text-[15px] font-medium ${colors.textMain}`}>正在思考</div>
                  <div className={`text-[15px] ${colors.textMuted}`}>正在生成更细致的图像——请稍候。</div>
                </div>
                <div
                  className={`relative aspect-square w-full overflow-hidden rounded-[34px] ${
                    isDark ? 'bg-[#303030]' : 'bg-[#e5e5e5]'
	                  }`}
	                >
	                  <div
	                    className={`image-placeholder-dotfield image-placeholder-dotfield-base absolute inset-0 ${
	                      isDark
	                        ? 'bg-[radial-gradient(circle_at_center,rgba(255,255,255,0.07)_0_1px,transparent_1.35px)]'
	                        : 'bg-[radial-gradient(circle_at_center,rgba(0,0,0,0.08)_0_1px,transparent_1.35px)]'
	                    } [background-size:18px_18px]`}
	                    style={{
	                      WebkitMaskImage: 'linear-gradient(135deg, rgba(0,0,0,0.22), rgba(0,0,0,0.72) 58%, rgba(0,0,0,0.92))',
	                      maskImage: 'linear-gradient(135deg, rgba(0,0,0,0.22), rgba(0,0,0,0.72) 58%, rgba(0,0,0,0.92))',
	                    }}
	                  />
	                  <div
	                    className={`image-placeholder-dotfield image-placeholder-dotfield-slow absolute inset-0 ${
	                      isDark
	                        ? 'bg-[radial-gradient(circle_at_58%_62%,rgba(255,255,255,0.12)_0_1px,transparent_1.4px)]'
	                        : 'bg-[radial-gradient(circle_at_58%_62%,rgba(0,0,0,0.14)_0_1px,transparent_1.4px)]'
	                    } [background-size:16px_16px]`}
	                    style={{
	                      WebkitMaskImage: 'radial-gradient(circle at 58% 62%, black 0 36%, transparent 62%)',
	                      maskImage: 'radial-gradient(circle at 58% 62%, black 0 36%, transparent 62%)',
	                    }}
	                  />
	                  <div
	                    className={`image-placeholder-dotfield image-placeholder-dotfield-fast absolute inset-0 ${
	                      isDark
	                        ? 'bg-[radial-gradient(circle_at_68%_72%,rgba(255,255,255,0.20)_0_1.7px,transparent_2px)]'
	                        : 'bg-[radial-gradient(circle_at_68%_72%,rgba(0,0,0,0.22)_0_1.7px,transparent_2px)]'
                    } [background-size:22px_22px]`}
                    style={{
                      WebkitMaskImage: 'radial-gradient(circle at 68% 72%, black 0 17%, transparent 35%)',
                      maskImage: 'radial-gradient(circle at 68% 72%, black 0 17%, transparent 35%)',
                    }}
                  />
                </div>
              </div>
            )
          }
          const isImageAsset = typeof asset?.url === 'string' && asset.url && String(asset?.mimeType ?? '').startsWith('image/')
          if (isImageAsset) {
            const absoluteImageUrl = typeof window === 'undefined' ? asset.url : new URL(asset.url, window.location.origin).toString()
            return (
              <div
                key={asset.id ?? asset.fileName ?? asset.url}
                className={`w-full max-w-[320px] overflow-hidden rounded-2xl border ${colors.border} ${isDark ? 'bg-[#171717]' : 'bg-[#fafafa]'}`}
              >
                <button
                  type="button"
                  onClick={() => setImagePreviewAsset({ url: asset.url, fileName: asset.fileName ?? '生成图片' })}
                  className="group relative block w-full overflow-hidden"
                >
                  <img src={asset.url} alt={asset.fileName ?? '生成图片'} className="block h-auto max-h-[360px] w-full max-w-[320px] object-cover transition-transform duration-200 group-hover:scale-[1.01]" />
                  <div className="pointer-events-none absolute inset-0 bg-gradient-to-t from-black/35 via-transparent to-transparent opacity-0 transition-opacity group-hover:opacity-100" />
                  <div className="pointer-events-none absolute right-3 top-3 flex items-center gap-1 rounded-full bg-black/55 px-2.5 py-1 text-xs text-white opacity-0 transition-opacity group-hover:opacity-100">
                    <Maximize2 className="h-3.5 w-3.5" />
                    查看大图
                  </div>
                </button>
                <div className={`flex flex-wrap items-center justify-between gap-2 border-t px-3 py-2 text-xs ${colors.border} ${colors.textMuted}`}>
                  <span className="min-w-0 flex-1 truncate">{asset.fileName ?? '生成图片'}</span>
                  <div className="flex shrink-0 items-center gap-2">
                    <button
                      type="button"
                      onClick={() => handleCopy(absoluteImageUrl, `image-copy-${asset.id ?? asset.url}`)}
                      className={`rounded-full border px-2.5 py-1 ${colors.border} ${colors.hover}`}
                    >
                      {copiedStates[`image-copy-${asset.id ?? asset.url}`] ? '已复制' : '复制'}
                    </button>
                    <a
                      href={asset.url}
                      download={asset.fileName ?? 'generated-image.png'}
                      className={`rounded-full border px-2.5 py-1 ${colors.border} ${colors.hover}`}
                    >
                      下载
                    </a>
                  </div>
                </div>
              </div>
            )
          }
          return (
            <div key={asset.id ?? asset.fileName} className={`rounded-full border px-3 py-1 text-xs ${colors.border} ${colors.textMuted}`}>
              {asset.fileName}
            </div>
          )
        })}
      </div>
    )
  }

  function renderArtifactDrawer() {
    if (!activeArtifact) {
      return null
    }
    const activeFile = artifactDraftFiles.find((file) => file.path === activeArtifactFilePath) ?? artifactDraftFiles[0]
    return (
      <div className="fixed inset-0 z-[90] flex bg-black/55">
        <div className={`ml-auto flex h-full w-full max-w-6xl flex-col border-l ${colors.border} ${colors.modalBg}`}>
          <div className={`flex items-center justify-between gap-3 border-b px-5 py-4 ${colors.border}`}>
            <div className="min-w-0">
              <div className="flex items-center gap-2 text-base font-semibold">
                <Code2 className="h-5 w-5" />
                {activeArtifact.title || '代码预览'}
              </div>
              <div className={`mt-1 text-xs ${colors.textMuted}`}>版本 {activeArtifact.version ?? 1} · 支持 HTML / SVG / React / Vue 沙盒预览</div>
            </div>
            <div className="flex shrink-0 items-center gap-2">
              <button onClick={() => void saveArtifactVersion()} className={`rounded-full border px-3 py-1.5 text-sm ${colors.border} ${colors.hover}`}>保存版本</button>
              <a href={`/chat/artifacts/${activeArtifact.id}/download`} className={`rounded-full border px-3 py-1.5 text-sm ${colors.border} ${colors.hover}`}>下载 ZIP</a>
              <button onClick={() => setActiveArtifact(null)} className={`rounded-full p-2 ${colors.hover}`}><X className="h-5 w-5" /></button>
            </div>
          </div>
          {artifactStatus && <div className={`border-b px-5 py-2 text-sm ${colors.border} ${artifactStatus.includes('失败') ? 'text-red-500' : colors.textMuted}`}>{artifactStatus}</div>}
          <div className="grid min-h-0 flex-1 grid-cols-1 md:grid-cols-[220px_minmax(0,1fr)]">
            <aside className={`border-r p-3 ${colors.border} ${isDark ? 'bg-[#171717]' : 'bg-[#fafafa]'}`}>
              <div className={`mb-2 text-xs font-semibold uppercase tracking-[0.16em] ${colors.textMuted}`}>文件树</div>
              <div className="space-y-1">
                {artifactDraftFiles.map((file) => (
                  <button
                    key={file.path}
                    onClick={() => setActiveArtifactFilePath(file.path)}
                    className={`flex w-full items-center gap-2 rounded-xl px-3 py-2 text-left text-sm ${file.path === activeFile?.path ? (isDark ? 'bg-white/10' : 'bg-black/10') : colors.hover}`}
                  >
                    <Terminal className="h-4 w-4 shrink-0" />
                    <span className="truncate">{file.path}</span>
                  </button>
                ))}
              </div>
            </aside>
            <div className="grid min-h-0 grid-rows-[minmax(0,1fr)_minmax(260px,42%)]">
              <div className={`min-h-0 border-b ${colors.border}`}>
                <div className={`flex items-center justify-between border-b px-4 py-2 text-xs ${colors.border} ${colors.textMuted}`}>
                  <span>{activeFile?.path || '未选择文件'}</span>
                  <span>编辑器</span>
                </div>
                <div className="h-full min-h-[280px]">
                  <Editor
                    theme={isDark ? 'vs-dark' : 'light'}
                    language={monacoLanguageForFile(activeFile)}
                    value={activeFile?.content ?? ''}
                    onChange={(value) => activeFile && updateArtifactFile(activeFile.path, value ?? '')}
                    options={{
                      minimap: { enabled: false },
                      fontSize: 14,
                      lineHeight: 22,
                      wordWrap: 'on',
                      scrollBeyondLastLine: false,
                      automaticLayout: true,
                    }}
                  />
                </div>
              </div>
              <div className="min-h-0">
                <div className={`flex items-center justify-between border-b px-4 py-2 text-xs ${colors.border} ${colors.textMuted}`}>
                  <span>沙盒预览</span>
                  <span>iframe 隔离运行</span>
                </div>
                <iframe
                  title="代码沙盒预览"
                  srcDoc={buildArtifactPreviewDocument(artifactDraftFiles, activeArtifact.entryFile)}
                  sandbox="allow-scripts allow-forms allow-modals"
                  className="h-full min-h-[260px] w-full bg-white"
                />
              </div>
            </div>
          </div>
        </div>
      </div>
    )
  }

  if (loading) {
    return <div className="min-h-screen bg-[#171717] text-white flex items-center justify-center">正在加载...</div>
  }

  return (
    <>
      {renderArtifactDrawer()}
      {pendingDeleteConversation && (
        <div className="fixed inset-0 z-[220] flex items-center justify-center bg-black/60 backdrop-blur-sm p-4">
          <div className={`w-full max-w-md rounded-2xl border px-6 py-6 shadow-2xl ${colors.modalBg} ${colors.border}`}>
            <h2 className={`text-lg font-semibold ${colors.textMain}`}>删除这条聊天？</h2>
            <p className={`mt-3 text-sm leading-6 ${colors.textMuted}`}>
              删除后将移除
              <span className={`mx-1 font-medium ${colors.textMain}`}>“{pendingDeleteConversation.title}”</span>
              及其全部消息，这个操作无法撤销。
            </p>
            <div className="mt-6 flex items-center justify-end gap-3">
              <button type="button" onClick={() => setPendingDeleteConversation(null)} className={`rounded-xl border px-4 py-2 text-sm font-medium ${colors.border} ${colors.hover}`}>
                取消
              </button>
              <button
                type="button"
                onClick={() => void handleDeleteConversation()}
                disabled={deletingConversationId === pendingDeleteConversation.id}
                className="rounded-xl bg-red-600 px-4 py-2 text-sm font-medium text-white hover:bg-red-700 disabled:cursor-not-allowed disabled:opacity-60"
              >
                {deletingConversationId === pendingDeleteConversation.id ? '删除中...' : '确认删除'}
              </button>
            </div>
          </div>
        </div>
      )}

      {showShareModal && (
        <div className="fixed inset-0 z-[230] flex items-center justify-center bg-black/60 backdrop-blur-sm p-4">
          <div className={`w-full max-w-[680px] overflow-hidden rounded-[28px] border shadow-[0_24px_80px_rgba(0,0,0,0.28)] ${isDark ? 'border-white/10 bg-[#2b2b2b]/95' : 'border-black/8 bg-white/95'}`}>
            <div className={`flex items-start justify-between gap-4 border-b px-6 py-5 ${isDark ? 'border-white/8' : 'border-black/6'}`}>
              <div className="flex min-w-0 items-start gap-3">
                {shareModalState !== 'default' && (
                  <button
                    type="button"
                    onClick={() => {
                      if (shareModalState === 'copy' || shareModalState === 'upgrade') {
                        setShareModalState('default')
                        return
                      }
                      if (shareModalState === 'collaboration-copy') {
                        setShareModalState('code')
                        return
                      }
                      setShareModalState('default')
                    }}
                    className={`mt-0.5 rounded-full p-2 ${colors.hover}`}
                  >
                    <ArrowLeft className="h-4 w-4" />
                  </button>
                )}
                <div className="min-w-0">
                  <h2 className={`text-[20px] font-semibold tracking-[-0.02em] ${colors.textMain}`}>
                    {shareModalState === 'copy'
                      ? '复制链接'
                      : shareModalState === 'code'
                        ? '开启协作'
                        : shareModalState === 'collaboration-copy'
                          ? '复制协作链接'
                          : shareModalState === 'upgrade'
                            ? '升级套餐'
                            : '分享聊天'}
                  </h2>
                  <p className={`mt-1 text-sm ${colors.textMuted}`}>
                    {shareModalState === 'copy'
                      ? '这是公开访问链接。'
                      : shareModalState === 'code'
                        ? '请输入协作码后生成协作链接。'
                        : shareModalState === 'collaboration-copy'
                          ? '协作链接已生成，可直接复制。'
                          : shareModalState === 'upgrade'
                            ? '当前套餐暂不支持协作。'
                            : '先预览内容，再复制链接或开启协作。'}
                  </p>
                </div>
              </div>
              <button onClick={() => setShowShareModal(false)} className={`rounded-full p-2 ${colors.hover}`}>
                <X className="h-5 w-5" />
              </button>
            </div>
            <div className="px-6 py-5">
              {shareModalState === 'default' && (
                <div className={`overflow-hidden rounded-[24px] border ${isDark ? 'border-white/8 bg-white/[0.02]' : 'border-black/8 bg-black/[0.015]'}`}>
                  <div className={`border-b px-5 py-4 ${isDark ? 'border-white/8' : 'border-black/6'}`}>
                    <div className="flex flex-wrap items-center gap-3">
                      <div className="text-base font-medium">{currentConversationTitle}</div>
                      <div className={`rounded-full border px-3 py-1 text-xs ${isDark ? 'border-white/10 bg-white/[0.03]' : 'border-black/8 bg-black/[0.02]'} ${colors.textMuted}`}>
                        {sharePreviewMessages.length} 条预览
                      </div>
                    </div>
                  </div>
                  <div className="max-h-[360px] space-y-3 overflow-y-auto px-5 py-5">
                    {sharePreviewMessages.length > 0 ? (
                      sharePreviewMessages.map((item) => {
                        const content = normalizeVisibleMessageContent(item.content)
                        const attachments = Array.isArray(item.attachments) ? item.attachments : []
                        return (
                          <div key={item.id} className={`flex ${item.role === 'user' ? 'justify-end' : 'justify-start'}`}>
                            <div className={`max-w-[88%] rounded-3xl px-4 py-3 text-sm leading-6 ${item.role === 'user' ? colors.userBubble : isDark ? 'bg-white/[0.04]' : 'bg-white'} ${colors.textMain}`}>
                              <div className={`mb-1 text-[11px] font-medium uppercase tracking-[0.12em] ${colors.textMuted}`}>
                                {item.role === 'user' ? '你' : 'Assistant'}
                              </div>
                              <div className="line-clamp-5 whitespace-pre-wrap break-words">{content || '仅包含附件内容'}</div>
                              {attachments.length > 0 && <div className={`mt-2 text-xs ${colors.textMuted}`}>附件 {attachments.length} 个</div>}
                            </div>
                          </div>
                        )
                      })
                    ) : (
                      <div className={`rounded-2xl border px-4 py-6 text-sm text-center ${colors.border} ${colors.textMuted}`}>这段聊天还没有可预览的内容。</div>
                    )}
                  </div>
                </div>
              )}

              {shareModalState === 'copy' && (
                <div className={`rounded-[24px] border p-4 ${isDark ? 'border-white/8 bg-white/[0.02]' : 'border-black/8 bg-black/[0.015]'}`}>
                  <div className={`mb-3 text-xs font-medium ${colors.textMuted}`}>分享链接</div>
                  <div className={`flex items-center gap-3 rounded-2xl border px-4 py-3 ${isDark ? 'border-white/8 bg-white/[0.03]' : 'border-black/8 bg-white'}`}>
                    <div className="min-w-0 flex-1 break-all text-sm">{conversationShare?.shareURL || '链接生成中...'}</div>
                    <button
                      type="button"
                      onClick={() => void handleCopy(buildConversationShareClipboardText(conversationShare?.shareURL ?? ''), 'conversation-share-copy')}
                      disabled={!conversationShare?.shareURL}
                      className={`shrink-0 rounded-full px-4 py-2 text-sm font-medium ${isDark ? 'bg-white text-black hover:bg-[#ececec]' : 'bg-black text-white hover:bg-[#222]'} ${!conversationShare?.shareURL ? 'cursor-not-allowed opacity-50' : ''}`}
                    >
                      {copiedStates['conversation-share-copy'] ? '已复制' : '复制'}
                    </button>
                  </div>
                </div>
              )}

              {shareModalState === 'code' && (
                <div className={`rounded-[24px] border p-4 ${isDark ? 'border-white/8 bg-white/[0.02]' : 'border-black/8 bg-black/[0.015]'}`}>
                  <div className={`mb-2 text-sm font-medium ${colors.textMain}`}>请输入协作码</div>
                  <input
                    value={shareForm.accessCode}
                    onChange={(event) => setShareForm((prev) => ({ ...prev, accessCode: event.target.value }))}
                    placeholder="设置协作码"
                    className={`w-full rounded-2xl border px-4 py-3 text-sm outline-none transition ${isDark ? 'border-white/8 bg-white/[0.04] text-white placeholder:text-[#8a8a8a] focus:border-white/20' : 'border-black/10 bg-black/[0.02] text-black placeholder:text-[#999] focus:border-black/20'}`}
                  />
                  <div className={`mt-2 text-xs ${colors.textMuted}`}>
                    {conversationShare?.collaborationLimit > 0
                      ? `当前套餐最多支持 ${conversationShare.collaborationLimit} 人协作，已加入 ${conversationShare.currentCollaborators ?? 0} 人。`
                      : '当前套餐暂不支持协作。'}
                  </div>
                </div>
              )}

              {shareModalState === 'collaboration-copy' && (
                <div className="space-y-4">
                  <div className={`rounded-[24px] border p-4 ${isDark ? 'border-white/8 bg-white/[0.02]' : 'border-black/8 bg-black/[0.015]'}`}>
                    <div className={`mb-3 text-xs font-medium ${colors.textMuted}`}>协作链接</div>
                    <div className={`flex items-center gap-3 rounded-2xl border px-4 py-3 ${isDark ? 'border-white/8 bg-white/[0.03]' : 'border-black/8 bg-white'}`}>
                      <div className="min-w-0 flex-1 break-all text-sm">{conversationShare?.shareURL || '链接生成中...'}</div>
                      <button
                        type="button"
                        onClick={() => void handleCopy(buildConversationCollaborationClipboardText(conversationShare?.shareURL ?? '', shareResultCollaborationCode), 'conversation-collaboration-copy')}
                        disabled={!conversationShare?.shareURL}
                        className={`shrink-0 rounded-full px-4 py-2 text-sm font-medium ${isDark ? 'bg-white text-black hover:bg-[#ececec]' : 'bg-black text-white hover:bg-[#222]'} ${!conversationShare?.shareURL ? 'cursor-not-allowed opacity-50' : ''}`}
                      >
                        {copiedStates['conversation-collaboration-copy'] ? '已复制' : '复制'}
                      </button>
                    </div>
                  </div>
                  <div className={`rounded-[24px] border px-4 py-4 ${isDark ? 'border-white/8 bg-white/[0.02]' : 'border-black/8 bg-black/[0.015]'}`}>
                    <div className={`text-xs font-medium ${colors.textMuted}`}>协作码</div>
                    <div className="mt-2 text-base font-semibold tracking-[0.08em]">{shareResultCollaborationCode || shareForm.accessCode.trim() || '未设置'}</div>
                  </div>
                </div>
              )}

              {shareFeedback && (
                <div className={`mt-4 text-sm ${shareFeedback.includes('失败') || shareFeedback.includes('协作码') ? 'text-red-500' : colors.textMuted}`}>{shareFeedback}</div>
              )}

              {shareModalState === 'upgrade' && (
                <div className={`mt-4 rounded-2xl border px-4 py-4 text-sm ${colors.border} ${colors.textMuted}`}>
                  您可升级套餐继续使用此功能。
                </div>
              )}
            </div>
            <div className={`flex gap-3 border-t px-6 py-4 ${isDark ? 'border-white/8 bg-black/10' : 'border-black/6 bg-black/[0.015]'}`}>
              {shareModalState === 'default' && (
                <>
                  <button
                    onClick={() => void handlePrepareConversationShareLink()}
                    disabled={isSavingShare || isLoadingConversationShare}
                    className={`flex-1 rounded-full border px-4 py-3 text-sm font-medium ${isDark ? 'border-white/10 bg-white/[0.03] text-white hover:bg-white/[0.06]' : 'border-black/10 bg-black/[0.02] text-black hover:bg-black/[0.04]'} ${isSavingShare || isLoadingConversationShare ? 'opacity-60 cursor-not-allowed' : ''}`}
                  >
                    {isSavingShare ? '生成中...' : '复制链接'}
                  </button>
                  <button
                    onClick={() => {
                      if (Number(conversationShare?.collaborationLimit ?? 0) <= 0) {
                        setShareModalState('upgrade')
                        setShareFeedback('')
                        return
                      }
                      setShareModalState('code')
                      setShareFeedback('')
                      setShareForm((prev) => ({ ...prev, collaborationEnabled: true, enabled: true }))
                    }}
                    disabled={isSavingShare || isLoadingConversationShare}
                    className={`flex-1 rounded-full px-4 py-3 text-sm font-medium ${isDark ? 'bg-white text-black hover:bg-[#f0f0f0]' : 'bg-black text-white hover:bg-[#222]'} ${isSavingShare || isLoadingConversationShare ? 'opacity-60 cursor-not-allowed' : ''}`}
                  >
                    申请协作
                  </button>
                </>
              )}
              {shareModalState === 'copy' && (
                <button
                  onClick={() => setShowShareModal(false)}
                  className={`w-full rounded-full px-4 py-3 text-sm font-medium ${isDark ? 'bg-white text-black hover:bg-[#ececec]' : 'bg-black text-white hover:bg-[#222]'}`}
                >
                  完成
                </button>
              )}
              {shareModalState === 'code' && (
                <button
                  onClick={() => void handlePrepareCollaborationShareLink()}
                  disabled={isSavingShare}
                  className={`w-full rounded-full px-4 py-3 text-sm font-medium ${isDark ? 'bg-white text-black hover:bg-[#ececec]' : 'bg-black text-white hover:bg-[#222]'} ${isSavingShare ? 'opacity-60 cursor-not-allowed' : ''}`}
                >
                  {isSavingShare ? '生成中...' : '复制协作链接'}
                </button>
              )}
              {shareModalState === 'collaboration-copy' && (
                <button
                  onClick={() => setShowShareModal(false)}
                  className={`w-full rounded-full px-4 py-3 text-sm font-medium ${isDark ? 'bg-white text-black hover:bg-[#ececec]' : 'bg-black text-white hover:bg-[#222]'}`}
                >
                  完成
                </button>
              )}
              {shareModalState === 'upgrade' && (
                <button
                  onClick={() => {
                    setShowShareModal(false)
                    navigateTo('plans')
                  }}
                  className={`w-full rounded-full px-4 py-3 text-sm font-medium ${isDark ? 'bg-white text-black hover:bg-[#ececec]' : 'bg-black text-white hover:bg-[#222]'}`}
                >
                  升级套餐
                </button>
              )}
            </div>
          </div>
        </div>
      )}

      {imagePreviewAsset && (
        <div className="fixed inset-0 z-[240] flex items-center justify-center bg-black/80 p-4 backdrop-blur-sm" onClick={() => setImagePreviewAsset(null)}>
          <div className="relative max-h-[92vh] max-w-[92vw]" onClick={(event) => event.stopPropagation()}>
            <button onClick={() => setImagePreviewAsset(null)} className="absolute right-3 top-3 z-10 rounded-full bg-black/55 p-2 text-white hover:bg-black/70">
              <X className="h-5 w-5" />
            </button>
            <img src={imagePreviewAsset.url} alt={imagePreviewAsset.fileName} className="max-h-[92vh] max-w-[92vw] rounded-3xl object-contain shadow-2xl" />
          </div>
        </div>
      )}

      {showLoginModal && (
        <div className="fixed inset-0 z-[200] flex items-center justify-center bg-black/60 backdrop-blur-sm p-4">
          <div className={`w-full max-w-md px-8 py-10 rounded-2xl shadow-2xl relative ${colors.modalBg} border ${colors.border}`}>
            <button
              onClick={() => {
                setShowLoginModal(false)
                resetAuthFlow(isLoginMode ? 'login' : 'register')
              }}
              className={`absolute top-4 right-4 p-2 rounded-full ${colors.textMuted} ${colors.hover}`}
            >
              <X className="w-5 h-5" />
            </button>
            <div className="flex justify-center mb-6">
              <img src={BRAND_LOGO_SRC} alt="Infinite-AI" className="h-24 w-24 rounded-[2rem] object-cover" />
            </div>
            <h1 className={`text-2xl font-bold text-center mb-2 ${colors.textMain}`}>
              {isPasswordResetMode ? (passwordResetStep === 'success' ? '密码已重置' : '重置密码') : isLoginMode ? '欢迎回来' : registerStep === 'success' ? '您已完成注册' : '创建您的账号'}
            </h1>
            <p className={`text-center text-sm mb-6 ${colors.textMuted}`}>
              {isPasswordResetMode
                ? passwordResetStep === 'identity'
                  ? '输入邮箱或手机号，并完成人机验证码后获取重置验证码。'
                  : passwordResetStep === 'password'
                    ? '输入验证码与新密码，再完成一次人机验证码。'
                    : '现在可以使用新密码登录。'
                : isLoginMode
                ? '支持邮箱或手机号 + 密码登录。'
                : registerStep === 'identity'
                  ? '填写以下信息完成注册。'
                  : registerStep === 'verify'
                    ? `请输入发送到${registerIdentifierLabel}的验证码。`
                    : registerStep === 'password'
                      ? '设置并确认你的登录密码。'
                      : registerStep === 'captcha'
                        ? '完成图形验证码校验。'
                        : '恭喜您成功加入 Infinite-AI。'}
            </p>
            {isPasswordResetMode ? (
              passwordResetStep === 'success' ? (
                <div className="space-y-5">
                  <div className={`rounded-2xl border px-5 py-5 text-center ${colors.inputBg} ${colors.border}`}>
                    <div className="flex justify-center mb-3">
                      <CheckCircle2 className="w-10 h-10 text-green-500" />
                    </div>
                    <div className={`text-lg font-semibold ${colors.textMain}`}>密码已重置</div>
                    <div className={`mt-2 text-sm leading-6 ${colors.textMuted}`}>请使用新密码重新登录。</div>
                  </div>
                  <button type="button" onClick={returnToLoginFromPasswordReset} className={`w-full py-3 text-sm font-medium rounded-md ${colors.btnPrimary}`}>
                    返回登录
                  </button>
                </div>
              ) : (
                <form onSubmit={handleAuthSubmit} className="space-y-4">
                  <input
                    value={authForm.identifier}
                    onChange={(event) => setAuthForm((prev) => ({ ...prev, identifier: event.target.value }))}
                    className={`w-full px-4 py-3 rounded-md border ${colors.inputBg} ${colors.border} ${colors.textMain}`}
                    placeholder="邮箱或手机号"
                    type="text"
                    required
                    disabled={passwordResetStep === 'password'}
                  />
                  {passwordResetStep === 'password' && (
                    <>
                      <div className={`rounded-2xl border px-4 py-3 text-xs leading-6 ${colors.border} ${colors.textMuted}`}>
                        验证码已发送到 {passwordResetVerificationState?.masked || authForm.identifier}。调试模式下请从后台系统日志查看验证码。
                      </div>
                      <input
                        value={authForm.verificationCode}
                        onChange={(event) => setAuthForm((prev) => ({ ...prev, verificationCode: event.target.value }))}
                        className={`w-full px-4 py-3 rounded-md border ${colors.inputBg} ${colors.border} ${colors.textMain}`}
                        placeholder="输入重置验证码"
                        inputMode="numeric"
                        required
                      />
                      <input
                        value={authForm.password}
                        onChange={(event) => setAuthForm((prev) => ({ ...prev, password: event.target.value }))}
                        className={`w-full px-4 py-3 rounded-md border ${colors.inputBg} ${colors.border} ${colors.textMain}`}
                        placeholder="新密码"
                        type="password"
                        required
                      />
                      <input
                        value={authForm.confirmPassword}
                        onChange={(event) => setAuthForm((prev) => ({ ...prev, confirmPassword: event.target.value }))}
                        className={`w-full px-4 py-3 rounded-md border ${colors.inputBg} ${colors.border} ${colors.textMain}`}
                        placeholder="确认新密码"
                        type="password"
                        required
                      />
                    </>
                  )}
                  {renderCaptchaChallenge()}
                  {authError && <div className="text-sm text-red-500 text-center">{authError}</div>}
                  <div className="flex items-center gap-3 pt-2">
                    <button type="button" onClick={returnToLoginFromPasswordReset} className={`rounded-md border px-4 py-3 text-sm font-medium ${colors.border} ${colors.hover}`}>
                      返回登录
                    </button>
                    <button type="submit" className={`flex-1 py-3 text-sm font-medium rounded-md ${colors.btnPrimary}`}>
                      {passwordResetStep === 'identity' ? '发送验证码' : '确认重置'}
                    </button>
                  </div>
                </form>
              )
            ) : isLoginMode ? (
              <form onSubmit={handleAuthSubmit} className="space-y-4">
                <input
                  value={authForm.identifier}
                  onChange={(event) => setAuthForm((prev) => ({ ...prev, identifier: event.target.value }))}
                  className={`w-full px-4 py-3 rounded-md border ${colors.inputBg} ${colors.border} ${colors.textMain}`}
                  placeholder="邮箱或手机号"
                  type="text"
                  required
                />
                <input
                  value={authForm.password}
                  onChange={(event) => setAuthForm((prev) => ({ ...prev, password: event.target.value }))}
                  className={`w-full px-4 py-3 rounded-md border ${colors.inputBg} ${colors.border} ${colors.textMain}`}
                  placeholder="密码"
                  type="password"
                  required
                />
                {renderCaptchaChallenge()}
                {authError && <div className="text-sm text-red-500 text-center">{authError}</div>}
                <div className="text-right">
                  <button type="button" onClick={startPasswordReset} className={`text-sm hover:underline ${colors.textMuted}`}>
                    忘记密码？
                  </button>
                </div>
                <button type="submit" className={`w-full py-3 text-sm font-medium rounded-md mt-4 ${colors.btnPrimary}`}>
                  继续
                </button>
              </form>
            ) : registerStep === 'success' ? (
              <div className="space-y-5">
                <div className={`rounded-2xl border px-5 py-5 text-center ${colors.inputBg} ${colors.border}`}>
                  <div className="flex justify-center mb-3">
                    <CheckCircle2 className="w-10 h-10 text-green-500" />
                  </div>
                  <div className={`text-lg font-semibold ${colors.textMain}`}>您已完成注册</div>
                  <div className={`mt-2 text-sm leading-6 ${colors.textMuted}`}>恭喜您成功加入 Infinite-AI。</div>
                </div>
                <button type="button" onClick={handleStartUsing} className={`w-full py-3 text-sm font-medium rounded-md ${colors.btnPrimary}`}>
                  开始使用
                </button>
              </div>
            ) : (
              <form onSubmit={handleAuthSubmit} className="space-y-4">
                {registerStep === 'identity' && (
                  <>
                    <input
                      value={authForm.displayName}
                      onChange={(event) => setAuthForm((prev) => ({ ...prev, displayName: event.target.value }))}
                      className={`w-full px-4 py-3 rounded-md border ${colors.inputBg} ${colors.border} ${colors.textMain}`}
                      placeholder="昵称"
                      required
                    />
                    <input
                      value={authForm.identifier}
                      onChange={(event) => setAuthForm((prev) => ({ ...prev, identifier: event.target.value }))}
                      className={`w-full px-4 py-3 rounded-md border ${colors.inputBg} ${colors.border} ${colors.textMain}`}
                      placeholder="输入邮箱或手机号"
                      type="text"
                      required
                    />
                  </>
                )}
                {registerStep === 'verify' && (
                  <>
                    <div className={`rounded-2xl border px-4 py-4 text-sm leading-6 ${colors.inputBg} ${colors.border}`}>
                      <div className={`font-medium ${colors.textMain}`}>验证码接收方式</div>
                      <div className={`mt-1 ${colors.textMuted}`}>当前将发送到 {registerVerificationState?.masked || normalizedRegisterIdentifier}</div>
                    </div>
                    <div className="flex gap-2">
                      <input
                        value={authForm.verificationCode}
                        onChange={(event) => setAuthForm((prev) => ({ ...prev, verificationCode: event.target.value }))}
                        className={`flex-1 px-4 py-3 rounded-md border ${colors.inputBg} ${colors.border} ${colors.textMain}`}
                        placeholder={`输入${registerIdentifierLabel}验证码`}
                        inputMode="numeric"
                        required
                      />
                      <button
                        type="button"
                        onClick={() => void handleSendRegisterCode()}
                        disabled={isSendingVerificationCode || phoneCodeCooldown > 0}
                        className={`shrink-0 px-4 rounded-md text-sm font-medium border ${colors.border} ${
                          isSendingVerificationCode || phoneCodeCooldown > 0
                            ? 'opacity-50 cursor-not-allowed'
                            : colors.hover
                        }`}
                      >
                        {phoneCodeCooldown > 0 ? `${phoneCodeCooldown}s` : '发送验证码'}
                      </button>
                    </div>
                    {registerVerificationState?.deliveryMode === 'test' && (
                      <div className={`rounded-2xl border px-4 py-3 text-xs leading-6 ${colors.border} ${colors.textMuted}`}>
                        调试模式已开启，验证码不会直接展示给用户。请联系管理员从后台系统日志查看验证码后完成注册或登录。
                      </div>
                    )}
                    {registerVerificationState?.previewCode && registerVerificationState?.deliveryMode !== 'test' && (
                      <div className={`rounded-2xl border px-4 py-3 text-xs leading-6 ${colors.border} ${colors.textMuted}`}>
                        当前环境返回了调试验证码。当前验证码：{registerVerificationState.previewCode}
                      </div>
                    )}
                    {!registerIdentifierIsEmail && !session?.authSecurity?.smsGatewayConfigured && !session?.authSecurity?.verificationTestMode && (
                      <div className="text-xs text-amber-500">当前未检测到短信网关配置；如果后台已开启调试模式，这里仍然可以直接发送，系统会自动写入后台日志。</div>
                    )}
                  </>
                )}
                {registerStep === 'password' && (
                  <>
                    <input
                      value={authForm.password}
                      onChange={(event) => setAuthForm((prev) => ({ ...prev, password: event.target.value }))}
                      className={`w-full px-4 py-3 rounded-md border ${colors.inputBg} ${colors.border} ${colors.textMain}`}
                      placeholder="创建密码"
                      type="password"
                      required
                    />
                    <input
                      value={authForm.confirmPassword}
                      onChange={(event) => setAuthForm((prev) => ({ ...prev, confirmPassword: event.target.value }))}
                      className={`w-full px-4 py-3 rounded-md border ${colors.inputBg} ${colors.border} ${colors.textMain}`}
                      placeholder="确认密码"
                      type="password"
                      required
                    />
                  </>
                )}
                {registerStep === 'captcha' && (
                  <>
                    {renderCaptchaChallenge('刷新人机验证')}
                  </>
                )}
                {authError && <div className="text-sm text-red-500 text-center">{authError}</div>}
                <div className="flex items-center gap-3 pt-2">
                  {registerStep !== 'identity' && (
                    <button type="button" onClick={handleRegisterBack} className={`flex items-center justify-center gap-2 rounded-md border px-4 py-3 text-sm font-medium ${colors.border} ${colors.hover}`}>
                      <ArrowLeft className="w-4 h-4" />
                      上一步
                    </button>
                  )}
                  <button type="submit" className={`flex-1 py-3 text-sm font-medium rounded-md ${colors.btnPrimary}`}>
                    {registerStep === 'captcha' || (!registerRequiresCaptcha && registerStep === 'password') ? '确认' : '下一步'}
                  </button>
                </div>
              </form>
            )}
            {isLoginMode && oauthProviders.length > 0 && (
              <>
                <div className={`my-6 flex items-center gap-3 text-xs uppercase tracking-[0.2em] ${colors.textMuted}`}>
                  <div className={`h-px flex-1 ${isDark ? 'bg-white/10' : 'bg-black/10'}`} />
                  继续使用
                  <div className={`h-px flex-1 ${isDark ? 'bg-white/10' : 'bg-black/10'}`} />
                </div>
                <div className="space-y-2">
                  {oauthProviders.map((provider) => (
                    <button
                      key={provider.slug}
                      type="button"
                      onClick={() => {
                        window.location.href = `/auth/oauth/start/${provider.slug}`
                      }}
                      className={`w-full flex items-center gap-3 rounded-xl border px-4 py-3 text-sm font-medium ${colors.border} ${colors.hover}`}
                    >
                      {provider.logoUrl ? (
                        <img src={provider.logoUrl} alt={provider.name} className="h-6 w-6 rounded-md object-cover" />
                      ) : (
                        <div className={`h-6 w-6 rounded-md flex items-center justify-center text-xs font-bold ${isDark ? 'bg-white text-black' : 'bg-black text-white'}`}>
                          {provider.name.slice(0, 1)}
                        </div>
                      )}
                      <span>使用 {provider.name} 继续</span>
                    </button>
                  ))}
                </div>
              </>
            )}
            {!isPasswordResetMode && registerStep !== 'success' && (
              <div className={`mt-6 text-center text-sm ${colors.textMuted}`}>
                {isLoginMode ? '还没有账号？' : '已经有账号？'}
                <button
                  onClick={() => {
                    resetAuthFlow(isLoginMode ? 'register' : 'login')
                  }}
                  className={`ml-1 hover:underline ${colors.textMain}`}
                >
                  {isLoginMode ? '注册' : '登录'}
                </button>
              </div>
            )}
          </div>
        </div>
      )}

      {showSettingsModal && (
        <div className="fixed inset-0 bg-black/60 z-[300] flex items-center justify-center p-4 backdrop-blur-sm">
          <div className={`w-full max-w-3xl rounded-2xl shadow-2xl border overflow-hidden flex flex-col md:flex-row h-[65vh] min-h-[450px] ${isDark ? 'bg-[#212121] text-[#ececec] border-[#333]' : 'bg-white text-black border-[#e5e5e5]'}`}>
            <div className={`w-full md:w-56 border-r p-3 overflow-y-auto ${isDark ? 'bg-[#171717] border-[#333]' : 'bg-[#f9f9f9] border-[#e5e5e5]'}`}>
              <h2 className={`px-3 py-2 text-sm font-semibold mb-2 ${colors.textMuted}`}>设置</h2>
              <button onClick={() => setSettingsTab('general')} className={`w-full text-left px-3 py-2.5 rounded-lg text-sm font-medium ${settingsTab === 'general' ? (isDark ? 'bg-[#2f2f2f]' : 'bg-[#e5e5e5]') : ''}`}>通用</button>
              <button onClick={() => setSettingsTab('data')} className={`w-full text-left px-3 py-2.5 rounded-lg text-sm font-medium mt-1 ${settingsTab === 'data' ? (isDark ? 'bg-[#2f2f2f]' : 'bg-[#e5e5e5]') : ''}`}>数据控制</button>
            </div>
            <div className={`flex-1 p-6 md:p-8 overflow-y-auto relative ${isDark ? 'bg-[#212121]' : 'bg-white'}`}>
              <button onClick={() => setShowSettingsModal(false)} className={`absolute top-4 right-4 p-2 rounded-lg ${colors.hover}`}>
                <X className="w-5 h-5" />
              </button>
              {settingsTab === 'general' && (
                <div className="space-y-6">
                  <h3 className={`text-lg font-medium mb-6 pb-4 border-b ${isDark ? 'border-[#333]' : 'border-[#e5e5e5]'}`}>通用</h3>
                  <div className={`flex items-center justify-between pb-4 border-b ${isDark ? 'border-[#333]' : 'border-[#e5e5e5]'}`}>
                    <div className="font-medium text-sm">主题</div>
                    <select value={theme} onChange={(event) => setTheme(event.target.value as ThemePreference)} className={`border rounded-lg px-3 py-2 text-sm ${colors.inputBg} ${colors.border}`}>
                      <option value="system">跟随系统</option>
                      <option value="dark">深色模式</option>
                      <option value="light">浅色模式</option>
                    </select>
                  </div>
                  <div className={`flex items-center justify-between pb-4 border-b ${isDark ? 'border-[#333]' : 'border-[#e5e5e5]'}`}>
                    <div className="font-medium text-sm">语言</div>
                    <select value={language} onChange={(event) => setLanguage(event.target.value)} className={`border rounded-lg px-3 py-2 text-sm ${colors.inputBg} ${colors.border}`}>
                      <option value="auto">自动检测</option>
                      <option value="zh-CN">简体中文</option>
                      <option value="en">English</option>
                    </select>
                  </div>
                  <button onClick={handleSaveSettings} className={`px-4 py-2 rounded-md text-sm font-medium ${colors.btnPrimary}`}>保存设置</button>
                </div>
              )}
              {settingsTab === 'data' && (
                <div className="space-y-6">
                  <h3 className={`text-lg font-medium mb-6 pb-4 border-b ${isDark ? 'border-[#333]' : 'border-[#e5e5e5]'}`}>数据控制</h3>
	                  <div className={`flex items-start justify-between gap-4 pb-4 border-b ${isDark ? 'border-[#333]' : 'border-[#e5e5e5]'}`}>
	                    <div>
	                      <div className="font-medium text-sm">保存聊天记录与上下文</div>
	                      <p className={`mt-2 text-xs leading-6 ${colors.textMuted}`}>
                        开启后，会保存你聊过的话题、消息内容和角色上下文，方便在最近聊天里继续对话。
                        关闭后，新对话会变成临时聊天，不会写入账号历史，刷新页面后也不会保留。
                      </p>
                    </div>
                    <label className="inline-flex cursor-pointer items-center">
                      <input
                        type="checkbox"
                        checked={chatHistoryEnabled}
                        onChange={(event) => setChatHistoryEnabled(event.target.checked)}
                        className="h-4 w-4"
	                      />
	                    </label>
	                  </div>
	                  <div className={`flex items-start justify-between gap-4 pb-4 border-b ${isDark ? 'border-[#333]' : 'border-[#e5e5e5]'}`}>
	                    <div>
	                      <div className="font-medium text-sm">账号级记忆</div>
	                      <p className={`mt-2 text-xs leading-6 ${colors.textMuted}`}>
	                        开启后，同一账号的新对话会参考你在其他对话里说过的信息、偏好和项目背景，体验接近 ChatGPT 的跨对话记忆。
	                        关闭后，助手只会记住当前单个对话里的上下文。
	                        {!chatHistoryEnabled && ' 当前未保存聊天记录，账号级记忆暂不会生效。'}
	                      </p>
	                    </div>
	                    <label className="inline-flex cursor-pointer items-center">
	                      <input
	                        type="checkbox"
	                        checked={memoryEnabled}
	                        onChange={(event) => setMemoryEnabled(event.target.checked)}
	                        className="h-4 w-4"
	                      />
	                    </label>
	                  </div>
	                  <div className={`flex items-center justify-between pb-4 border-b ${isDark ? 'border-[#333]' : 'border-[#e5e5e5]'}`}>
	                    <div className="font-medium text-sm">清除所有聊天记录</div>
	                    <button onClick={handleClearChats} className="px-4 py-2 rounded-lg text-sm font-medium text-red-500 border border-red-500/50 hover:bg-red-500/10">清除</button>
                  </div>
                  <div className={`flex items-center justify-between pb-4 border-b ${isDark ? 'border-[#333]' : 'border-[#e5e5e5]'}`}>
                    <div className="font-medium text-sm">导出数据</div>
                    <button onClick={handleExportData} className={`px-4 py-2 rounded-lg text-sm font-medium border ${colors.border}`}>导出</button>
                  </div>
                  <div className="flex items-center justify-between pb-4">
                    <div className="font-medium text-sm text-red-500">删除账号</div>
                    <button onClick={handleDeleteAccount} className="px-4 py-2 rounded-lg text-sm font-medium bg-red-600 text-white hover:bg-red-700">删除</button>
                  </div>
                </div>
              )}
            </div>
          </div>
        </div>
      )}

      {view === 'plans' && (
        <div className={`flex flex-col h-screen overflow-y-auto ${colors.appBg} ${colors.textMain}`}>
          <div className={`h-14 flex items-center px-4 md:px-8 border-b sticky top-0 z-20 ${colors.appBg} ${colors.border}`}>
            <button onClick={() => navigateTo('chat')} className={`flex items-center gap-2 text-sm font-medium ${colors.textMuted}`}>
              <ArrowLeft className="w-4 h-4" /> 返回聊天
            </button>
          </div>
          <div className="flex-1 max-w-7xl mx-auto w-full p-6 md:p-12">
            <h1 className="text-3xl font-bold mb-10 text-center">升级您的套餐</h1>
            <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-4 gap-6">
              {plans.map((plan) => {
                const active = subscription?.planCode === plan.code
                const isProMax = plan.code === 'pro_max'
                return (
                  <div key={plan.code} className={`rounded-2xl p-6 border flex flex-col ${colors.modalInner} ${plan.code === 'plus' ? 'border-purple-500 border-2' : colors.border}`}>
                    <h3 className={`text-xl font-semibold mb-2 ${plan.code === 'go' ? 'text-blue-500' : plan.code.startsWith('pro') ? 'text-orange-500' : ''}`}>{plan.name}</h3>
                    <div className={`text-sm mb-6 ${colors.textMuted}`}>{plan.description}</div>
                    <div className="text-3xl font-bold mb-6">
                      ¥{(plan.priceCents / 100).toFixed(0)}
                      <span className={`text-base font-normal ${colors.textMuted}`}>/月</span>
                    </div>
                    <button
                      onClick={() => !active && openPlanCheckout(plan.code)}
                      className={`w-full py-3 text-sm font-semibold rounded-xl mb-6 ${active ? `border ${colors.border} ${colors.textMuted}` : colors.btnPrimary}`}
                    >
                      {active ? '当前套餐' : '升级'}
                    </button>
                    <div className="text-sm font-medium mb-4">包含权益：</div>
                    <ul className="space-y-4 text-sm flex-1">
                      {plan.features?.map((feature: string) => (
                        <li key={feature} className="flex items-start gap-3">
                          <Check className={`w-5 h-5 shrink-0 ${isProMax ? 'text-orange-500' : plan.code === 'plus' ? 'text-purple-500' : colors.textMain}`} />
                          <span>{feature}</span>
                        </li>
                      ))}
                    </ul>
                  </div>
                )
              })}
            </div>
          </div>
        </div>
      )}

      {view === 'payment' && (
        <div className={`flex flex-col h-screen overflow-y-auto ${isDark ? 'bg-black text-white' : 'bg-[#f4f4f5] text-black'}`}>
          <div className="flex-1 flex flex-col md:flex-row">
            <div className="w-full md:w-[45%] p-8 md:p-16 flex flex-col bg-[#111111] text-white">
              <button onClick={() => navigateTo('chat')} className="flex items-center gap-2 text-sm font-medium text-gray-400 w-fit mb-12">
                <ArrowLeft className="w-4 h-4" /> 返回
              </button>
              <div className="flex items-center gap-2 font-medium text-gray-400 mb-6">
                <img src={BRAND_LOGO_SRC} alt="Infinite-AI" className="h-10 w-10 rounded-2xl object-cover" /> <span>Infinite-AI</span>
              </div>
              <p className="text-gray-400 text-sm mb-2">{checkoutData?.desc ?? '按月订阅'}</p>
              <h1 className="text-3xl md:text-4xl font-medium mb-6 text-white">{checkoutData?.title ?? '结账'}</h1>
              <div className="text-5xl font-medium mb-12 text-white">
                <span className="text-3xl mr-1 font-normal text-gray-400">¥</span>
                {checkoutData?.amount ?? '0.00'}
              </div>
            </div>
            <div className={`w-full md:w-[55%] p-8 md:p-16 ${isDark ? 'bg-black' : 'bg-white'} flex justify-center`}>
              <div className="w-full max-w-md">
                <h2 className="text-xl font-medium mb-6">结账</h2>
                <div className="space-y-6">
                  <div>
                    <label className="block text-sm font-medium mb-2">联系邮箱</label>
                    <input type="email" defaultValue={session?.user?.email ?? ''} disabled className={`w-full px-4 py-3 rounded-md border opacity-60 ${isDark ? 'bg-[#1a1a1a] border-[#333] text-white' : 'bg-gray-50 border-gray-300 text-black'}`} />
                  </div>
                  <div>
                    <label className="flex items-center gap-2 text-sm font-medium mb-2">
                      支付方式 <span className="bg-gray-200 text-gray-800 text-[10px] px-1.5 py-0.5 rounded-sm uppercase font-bold">IF-Pay</span>
                    </label>
                    <div className={`rounded-md border overflow-hidden ${isDark ? 'border-[#333]' : 'border-gray-300'}`}>
                      {([
                        ['wechat', '微信支付', 'text-green-500', ShieldCheck],
                        ['alipay', '支付宝', 'text-blue-500', ShieldCheck],
                        ['usdt', '加密货币', 'text-orange-500', Coins],
                      ] as Array<[string, string, string, ComponentType<{ className?: string }>]>).map(([value, label, className, Icon]) => (
                        <label key={value} className={`flex items-center gap-3 p-4 border-b cursor-pointer ${selectedPaymentMethod === value ? (isDark ? 'bg-[#1a1a1a]' : 'bg-gray-50') : ''} ${isDark ? 'border-[#333]' : 'border-gray-300'}`}>
                          <input type="radio" checked={selectedPaymentMethod === value} onChange={() => setSelectedPaymentMethod(value)} className="w-4 h-4" />
                          <Icon className={`w-5 h-5 ${className}`} />
                          <span className="text-sm font-medium">{label}</span>
                        </label>
                      ))}
                    </div>
                  </div>
                  <div className={`text-xs flex items-center gap-1.5 ${colors.textMuted}`}>
                    <Lock className="w-3.5 h-3.5" /> 支付信息已通过 SSL 加密传输。
                  </div>
                  {paymentFeedback && <div className="text-sm text-amber-500">{paymentFeedback}</div>}
                  <button onClick={handlePay} className="w-full py-4 rounded-md text-base font-medium bg-blue-600 hover:bg-blue-700 text-white">
                    支付 ¥{checkoutData?.amount ?? '0.00'}
                  </button>
                </div>
              </div>
            </div>
          </div>
        </div>
      )}

      {(view === 'api' || view === 'api-docs') && (
        <div className={`flex h-screen overflow-hidden ${isDark ? 'bg-[#171717]' : 'bg-[#f4f4f4]'} ${colors.textMain}`}>
          <div className={`w-64 border-r flex flex-col ${isDark ? 'bg-[#171717] border-[#333]' : 'bg-[#f4f4f4] border-[#e5e5e5]'}`}>
            <div className="h-14 flex items-center gap-3 px-4 shrink-0 font-medium tracking-wide">
              <img src={BRAND_LOGO_SRC} alt="Infinite-AI" className="h-12 w-12 rounded-2xl object-cover" />
              <span>Infinite-AI 开发者平台</span>
            </div>
            <div className="px-3 py-4 flex-1 space-y-1">
              <div className={`text-xs font-semibold px-2 mb-2 mt-2 ${colors.textMuted}`}>控制台</div>
              <button onClick={() => navigateTo('api')} className={`w-full flex items-center gap-3 px-3 py-2 text-sm rounded-md ${view === 'api' ? (isDark ? 'bg-[#212121] text-white' : 'bg-white') : ''}`}>
                <Code2 className="w-4 h-4" /> API Keys
              </button>
              <button onClick={() => navigateTo('api-docs')} className={`w-full flex items-center gap-3 px-3 py-2 text-sm rounded-md ${view === 'api-docs' ? (isDark ? 'bg-[#212121] text-white' : 'bg-white') : ''}`}>
                <BookOpen className="w-4 h-4" /> 官方文档
              </button>
              <button onClick={() => navigateTo('chat')} className={`w-full flex items-center gap-3 px-3 py-2 text-sm rounded-md ${colors.sidebarHover}`}>
                <ArrowLeft className="w-4 h-4" /> 返回聊天界面
              </button>
            </div>
          </div>
          <div className={`flex-1 flex flex-col ${isDark ? 'bg-[#212121]' : 'bg-white'}`}>
            <div className={`h-14 flex items-center px-8 border-b text-sm font-medium ${isDark ? 'border-[#333]' : 'border-[#e5e5e5]'}`}>{view === 'api' ? 'API Keys' : 'Documentation'}</div>
            <div className="flex-1 overflow-y-auto p-8 md:p-12">
              <div className="max-w-4xl">
                {view === 'api' ? (
                  <>
                    <h1 className="text-3xl font-medium mb-4">API Keys</h1>
                    <p className={`text-sm mb-10 ${colors.textMuted}`}>这里展示的是当前站点真实可访问的开发者入口。系统走了反代后，API URL 直接使用当前域名的 `/v1`，Key 也会直接用于真实请求鉴权。</p>
                    <div className="mb-10">
                      <h2 className="text-lg font-medium mb-4">Base URL</h2>
                      <div className={`flex items-center justify-between px-4 py-3 rounded-md border ${colors.inputBg} ${colors.border}`}>
                        <code className="text-sm font-mono tracking-wide">{publicAPIBaseURL}/v1</code>
                        <button onClick={() => handleCopy(`${publicAPIBaseURL}/v1`, 'url')} className={`text-sm flex items-center gap-1.5 ${colors.textMuted}`}>
                          {copiedStates.url ? <CheckCircle2 className="w-4 h-4 text-green-500" /> : <Copy className="w-4 h-4" />}
                        </button>
                      </div>
                    </div>
                    <div className={`border rounded-md overflow-hidden mb-6 ${colors.border}`}>
                      <table className="w-full text-left text-sm">
                        <thead className={`border-b ${colors.border} ${isDark ? 'bg-[#1a1a1a]' : 'bg-[#f9f9f9]'}`}>
                          <tr>
                            <th className="px-4 py-3 font-medium text-xs tracking-wider">名称</th>
                            <th className="px-4 py-3 font-medium text-xs tracking-wider">Secret Key</th>
                            <th className="px-4 py-3 font-medium text-xs tracking-wider">创建时间</th>
                            <th className="px-4 py-3 font-medium text-xs tracking-wider text-right">操作</th>
                          </tr>
                        </thead>
                        <tbody>
                          {apiKeys.map((item) => (
                            <tr key={item.id}>
                              <td className="px-4 py-4">{item.name}</td>
                              <td className="px-4 py-4 font-mono text-gray-500">{item.revealedKey || `${item.prefix}••••••••`}</td>
                              <td className="px-4 py-4 text-gray-500">{new Date(item.createdAt).toLocaleString()}</td>
                              <td className="px-4 py-4 text-right">
                                <button onClick={() => api.revokeApiKey(item.id).then(() => bootstrap())} className={`p-1.5 rounded ${colors.hover}`}>撤销</button>
                              </td>
                            </tr>
                          ))}
                        </tbody>
                      </table>
                    </div>
                    <button onClick={handleCreateApiKey} className={`px-4 py-2 text-sm font-medium rounded-md ${colors.btnPrimary}`}>创建新的 Secret Key</button>
                  </>
                ) : (
                  <>
                    <h1 className="text-3xl font-medium mb-4">官方文档</h1>
                    <p className={`text-base leading-relaxed mb-8 ${colors.textMuted}`}>开发者平台已经分成 OpenAI 和 Anthropic 两种接入方式，下面给你分别列出真实 URL、鉴权方式、调用示例和官方文档入口。</p>
                    <div className="grid gap-6 md:grid-cols-2">
                      <div className={`rounded-2xl border p-6 ${colors.border} ${isDark ? 'bg-[#171717]' : 'bg-[#fafafa]'}`}>
                        <div className="flex items-center justify-between gap-3">
                          <div>
                            <h2 className="text-xl font-medium">OpenAI 兼容</h2>
                            <p className={`mt-2 text-sm leading-6 ${colors.textMuted}`}>使用 `Authorization: Bearer`，直接访问 `/v1/chat/completions` 或图片生成接口。</p>
                          </div>
                          <BookOpen className="h-5 w-5 shrink-0" />
                        </div>
                        <div className={`mt-5 rounded-2xl border px-4 py-3 text-sm ${colors.border}`}>
                          <div className={`text-xs uppercase tracking-[0.18em] ${colors.textMuted}`}>API URL</div>
                          <code className="mt-2 block break-all font-mono">{publicAPIBaseURL}/v1</code>
                        </div>
                        <pre className={`mt-5 rounded-2xl p-4 text-sm font-mono overflow-x-auto ${isDark ? 'bg-[#0d0d0d] text-green-400' : 'bg-gray-50 text-green-700'}`}>
{`curl ${publicAPIBaseURL}/v1/chat/completions \\
  -H "Authorization: Bearer YOUR_API_KEY" \\
  -H "Content-Type: application/json" \\
  -d '{
    "model": "infinite-ai-standard",
    "messages": [{"role":"user","content":"你好"}]
  }'`}
                        </pre>
                        <div className="mt-5 flex flex-wrap gap-3">
                          <a href={OPENAI_DOCS_URL} target="_blank" rel="noreferrer" className={`rounded-xl px-4 py-2 text-sm font-medium ${colors.btnPrimary}`}>OpenAI 官方总览</a>
                          <a href={OPENAI_API_REFERENCE_URL} target="_blank" rel="noreferrer" className={`rounded-xl border px-4 py-2 text-sm font-medium ${colors.border} ${colors.hover}`}>OpenAI API Reference</a>
                        </div>
                      </div>
                      <div className={`rounded-2xl border p-6 ${colors.border} ${isDark ? 'bg-[#171717]' : 'bg-[#fafafa]'}`}>
                        <div className="flex items-center justify-between gap-3">
                          <div>
                            <h2 className="text-xl font-medium">Anthropic 兼容</h2>
                            <p className={`mt-2 text-sm leading-6 ${colors.textMuted}`}>使用 `x-api-key` 和 `anthropic-version`，直接访问 `/v1/messages`。</p>
                          </div>
                          <BookOpen className="h-5 w-5 shrink-0" />
                        </div>
                        <div className={`mt-5 rounded-2xl border px-4 py-3 text-sm ${colors.border}`}>
                          <div className={`text-xs uppercase tracking-[0.18em] ${colors.textMuted}`}>API URL</div>
                          <code className="mt-2 block break-all font-mono">{publicAPIBaseURL}/v1/messages</code>
                        </div>
                        <pre className={`mt-5 rounded-2xl p-4 text-sm font-mono overflow-x-auto ${isDark ? 'bg-[#0d0d0d] text-green-400' : 'bg-gray-50 text-green-700'}`}>
{`curl ${publicAPIBaseURL}/v1/messages \\
  -H "x-api-key: YOUR_API_KEY" \\
  -H "anthropic-version: 2023-06-01" \\
  -H "Content-Type: application/json" \\
  -d '{
    "model": "infinite-ai-standard",
    "max_tokens": 1024,
    "messages": [{"role":"user","content":"你好"}]
  }'`}
                        </pre>
                        <div className="mt-5 flex flex-wrap gap-3">
                          <a href={ANTHROPIC_DOCS_URL} target="_blank" rel="noreferrer" className={`rounded-xl px-4 py-2 text-sm font-medium ${colors.btnPrimary}`}>Anthropic 官方总览</a>
                          <a href={ANTHROPIC_API_REFERENCE_URL} target="_blank" rel="noreferrer" className={`rounded-xl border px-4 py-2 text-sm font-medium ${colors.border} ${colors.hover}`}>Anthropic Messages API</a>
                        </div>
                      </div>
                    </div>
                    <div className="mt-6 grid gap-4 md:grid-cols-2">
                      <button onClick={() => navigateTo('infinite-code')} className={`rounded-2xl border p-5 text-left ${colors.border} ${colors.hover}`}>
                        <div className="flex items-center gap-3">
                          <Terminal className="h-5 w-5" />
                          <span className="text-lg font-medium">Infinite Code</span>
                        </div>
                        <p className={`mt-3 text-sm leading-6 ${colors.textMuted}`}>点击进入 `http://127.0.0.1:1001/infinite-code`，查看当前套餐的 Infinite Code 周期额度与使用情况。</p>
                      </button>
                      <button onClick={() => navigateTo('download')} className={`rounded-2xl border p-5 text-left ${colors.border} ${colors.hover}`}>
                        <div className="flex items-center gap-3">
                          <Download className="h-5 w-5" />
                          <span className="text-lg font-medium">下载入口</span>
                        </div>
                        <p className={`mt-3 text-sm leading-6 ${colors.textMuted}`}>点击前往 `http://127.0.0.1:1001/download`，按当前主题样式查看下载栏。</p>
                      </button>
                    </div>
                  </>
                )}
              </div>
            </div>
          </div>
        </div>
      )}

      {view === 'download' && (
        <div className={`flex flex-col h-screen overflow-y-auto ${colors.appBg} ${colors.textMain}`}>
          <div className={`h-14 flex items-center px-4 md:px-8 border-b sticky top-0 z-20 ${colors.appBg} ${colors.border}`}>
            <button onClick={() => navigateTo('chat')} className={`flex items-center gap-2 text-sm font-medium ${colors.textMuted}`}>
              <ArrowLeft className="w-4 h-4" /> 返回聊天
            </button>
          </div>
          <div className="flex-1 overflow-y-auto p-8 md:p-16">
            <div className="max-w-3xl mx-auto">
              <h1 className="text-3xl font-medium mb-10 text-center">下载客户端</h1>
              <div className="space-y-6">
                {downloads.map((item) => (
                  <div key={item.id} className={`flex items-center justify-between p-6 rounded-xl border ${colors.border}`}>
                    <div className="flex items-center gap-4">
                      {item.platform === 'desktop' ? <Monitor className="w-8 h-8" /> : <Smartphone className="w-8 h-8" />}
                      <div>
                        <h3 className="font-medium text-lg">{item.title}</h3>
                        <p className={`text-sm ${colors.textMuted}`}>{item.description}</p>
                      </div>
                    </div>
                    {item.downloadUrl ? (
                      <a href={item.downloadUrl} className={`px-4 py-2 rounded-md text-sm font-medium ${colors.btnPrimary}`}>
                        下载
                      </a>
                    ) : (
                      <button disabled className="px-4 py-2 rounded-md text-sm font-medium bg-gray-500/20 text-gray-500 cursor-not-allowed">
                        未发布
                      </button>
                    )}
                  </div>
                ))}
              </div>
            </div>
          </div>
        </div>
      )}

      {view === 'infinite-code' && (
        <div className={`flex flex-col h-screen overflow-y-auto ${colors.appBg} ${colors.textMain}`}>
          <div className={`h-14 flex items-center px-4 md:px-8 border-b sticky top-0 z-20 ${colors.appBg} ${colors.border}`}>
            <button onClick={() => navigateTo('chat')} className={`flex items-center gap-2 text-sm font-medium ${colors.textMuted}`}>
              <ArrowLeft className="w-4 h-4" /> 返回聊天
            </button>
          </div>
          <div className="flex-1 overflow-y-auto p-8 md:p-16">
            <div className="max-w-3xl mx-auto">
              <div className="flex items-center gap-4 mb-10 pb-6 border-b" style={{ borderColor: isDark ? '#333' : '#e5e5e5' }}>
                <div className={`w-12 h-12 rounded-lg flex items-center justify-center border ${colors.border}`}>
                  <Terminal className="w-6 h-6" />
                </div>
                <div>
                  <h1 className="text-2xl font-medium">Infinite Code 编程助手</h1>
                  <p className={`text-sm mt-1 ${colors.textMuted}`}>IDE 专用插件额度与状态</p>
                </div>
              </div>
              <div className={`p-6 rounded-xl border mb-10 ${colors.border}`}>
                <h2 className="text-lg font-medium mb-6">当前配额使用情况</h2>
                <div className="grid gap-4 md:grid-cols-2">
                  <div className={`rounded-2xl border p-4 ${colors.border}`}>
                    <div className={`text-xs uppercase tracking-[0.18em] ${colors.textMuted}`}>当前套餐</div>
                    <div className="mt-2 text-xl font-medium">{usage?.infiniteCode?.planName ?? activePlanName}</div>
                    <div className={`mt-2 text-sm ${colors.textMuted}`}>后台可以设置每次恢复多少额度，以及多久恢复一次。</div>
                  </div>
                  <div className={`rounded-2xl border p-4 ${colors.border}`}>
                    <div className={`text-xs uppercase tracking-[0.18em] ${colors.textMuted}`}>下次恢复时间</div>
                    <div className="mt-2 text-xl font-medium">
                      {usage?.infiniteCode?.nextResetAt ? new Date(usage.infiniteCode.nextResetAt).toLocaleString() : '-'}
                    </div>
                    <div className={`mt-2 text-sm ${colors.textMuted}`}>每 {usage?.infiniteCode?.resetHours ?? 24} 小时恢复一次</div>
                  </div>
                </div>
                <div className="mt-6 flex justify-between text-sm mb-2">
                  <span className="font-medium">本周期剩余</span>
                  <span className="font-mono">{usage?.infiniteCode?.remaining ?? 0} / {usage?.infiniteCode?.credits ?? 0}</span>
                </div>
                <div className={`w-full h-2 rounded-full overflow-hidden mb-3 ${isDark ? 'bg-[#333]' : 'bg-gray-200'}`}>
                  <div
                    className="h-full bg-blue-500 rounded-full"
                    style={{
                      width: `${Math.min(
                        100,
                        Math.round(
                          ((Number(usage?.infiniteCode?.remaining ?? 0) || 0) / Math.max(Number(usage?.infiniteCode?.credits ?? 0) || 1, 1)) * 100,
                        ),
                      )}%`,
                    }}
                  />
                </div>
                <div className="grid gap-3 text-sm md:grid-cols-3">
                  <div className={`rounded-xl border px-4 py-3 ${colors.border}`}>
                    <div className={colors.textMuted}>每次恢复额度</div>
                    <div className="mt-1 font-medium">{usage?.infiniteCode?.credits ?? 0} 次</div>
                  </div>
                  <div className={`rounded-xl border px-4 py-3 ${colors.border}`}>
                    <div className={colors.textMuted}>本周期已使用</div>
                    <div className="mt-1 font-medium">{usage?.infiniteCode?.used ?? 0} 次</div>
                  </div>
                  <div className={`rounded-xl border px-4 py-3 ${colors.border}`}>
                    <div className={colors.textMuted}>本周期剩余</div>
                    <div className="mt-1 font-medium">{usage?.infiniteCode?.remaining ?? 0} 次</div>
                  </div>
                </div>
                <p className={`mt-4 text-xs ${colors.textMuted}`}>{usage?.infiniteCode?.hint ?? '未配置额度'}</p>
              </div>
              <h2 className="text-lg font-medium mb-4">补充配额 (单独充值)</h2>
              <div className={`p-6 rounded-xl border ${colors.border}`}>
                <div className="space-y-6">
                  <div className="flex flex-wrap gap-3">
                    {['100', '170', '240'].map((amt) => (
                      <button key={amt} onClick={() => setCustomRechargeAmount(amt)} className={`px-6 py-2 rounded-md border text-sm font-medium ${customRechargeAmount === amt ? 'border-blue-500 text-blue-500 bg-blue-500/10' : `${colors.border}`}`}>
                        ¥{amt}
                      </button>
                    ))}
                  </div>
                  <div className="relative max-w-sm">
                    <span className="absolute left-3 top-1/2 -translate-y-1/2 font-mono">¥</span>
                    <input value={customRechargeAmount} onChange={(event) => setCustomRechargeAmount(event.target.value)} className={`w-full pl-8 pr-4 py-2 rounded-md border ${colors.inputBg} ${colors.border}`} />
                  </div>
                  <button onClick={() => { setCheckoutData({ title: 'Infinite Code 算力额度', amount: customRechargeAmount, type: 'recharge', desc: '一次性充值' }); navigateTo('payment') }} className={`px-6 py-2.5 rounded-md text-sm font-medium ${colors.btnPrimary}`}>
                    继续结账
                  </button>
                </div>
              </div>
            </div>
          </div>
        </div>
      )}

      {view === 'shared-chat' && (
        <div className={`flex min-h-[100dvh] flex-col ${colors.appBg} ${colors.textMain}`}>
          <div className={`sticky top-0 z-20 border-b backdrop-blur-xl ${isDark ? 'border-white/8 bg-[#212121]/82' : 'border-black/6 bg-white/82'}`}>
            <div className="mx-auto flex h-14 max-w-4xl items-center justify-between px-4">
              <button onClick={() => navigate('/')} className={`flex items-center gap-2 text-sm font-medium ${colors.textMuted}`}>
                <ArrowLeft className="w-4 h-4" /> 返回首页
              </button>
              <div className="flex items-center gap-2">
                {sharedConversation?.collaborationEnabled ? (
                  <div className={`inline-flex items-center gap-1 rounded-full border px-3 py-1 text-xs ${isDark ? 'border-white/10 bg-white/[0.03]' : 'border-black/8 bg-black/[0.02]'} ${colors.textMuted}`}>
                    <Users className="h-3.5 w-3.5" />
                    协作上限 {sharedConversation?.currentCollaborators ?? 0}/{sharedConversation?.collaborationLimit ?? 0}
                  </div>
                ) : (
                  <div className={`inline-flex items-center gap-1 rounded-full border px-3 py-1 text-xs ${isDark ? 'border-white/10 bg-white/[0.03]' : 'border-black/8 bg-black/[0.02]'} ${colors.textMuted}`}>
                    公开只读分享
                  </div>
                )}
              </div>
            </div>
          </div>
          <div className="flex-1 px-4 pb-8">
            <div className="mx-auto max-w-4xl pt-6">
              {sharedChatLoading ? (
                <div className={`mx-auto max-w-xl rounded-[28px] border px-6 py-8 text-center text-sm ${isDark ? 'border-white/8 bg-white/[0.03]' : 'border-black/8 bg-black/[0.02]'} ${colors.textMuted}`}>正在加载分享内容...</div>
              ) : sharedConversation ? (
                <>
                  <div className={`mb-6 rounded-[28px] border px-6 py-5 ${isDark ? 'border-white/8 bg-white/[0.03]' : 'border-black/8 bg-black/[0.02]'}`}>
                    <div className="flex flex-wrap items-center gap-3">
                      <div className="text-[24px] font-semibold tracking-[-0.02em]">{sharedConversation.title || '对话分享'}</div>
                      {sharedConversation.ownerDisplayName && (
                        <div className={`rounded-full border px-3 py-1 text-xs ${isDark ? 'border-white/10 bg-white/[0.03]' : 'border-black/8 bg-black/[0.02]'} ${colors.textMuted}`}>来自 {sharedConversation.ownerDisplayName}</div>
                      )}
                    </div>
                    <div className={`mt-2 text-sm leading-6 ${colors.textMuted}`}>
                      {sharedConversation.collaborationEnabled
                        ? '此分享已开启协作，登录后可继续参与当前对话。'
                        : '此分享为只读公开查看。'}
                    </div>
                  </div>
                  <div className="space-y-6">
                    {sharedMessages.map((msg) => (
                      <div id={`message-${msg.id}`} key={msg.id} className={`flex w-full ${msg.role === 'user' ? 'justify-end' : 'justify-start'}`}>
                        {isAssistantRole(msg.role) ? (
                          <div className="flex gap-4 max-w-full">
                            <div className={`w-8 h-8 rounded-full flex items-center justify-center shrink-0 border ${colors.modalBg} ${colors.border}`}>
                              <img src={BRAND_LOGO_SRC} alt="Infinite-AI" className="h-6 w-6 rounded-xl object-cover" />
                            </div>
                            <div className="pt-1.5 min-w-0">
                              {renderReasoningPanel(msg)}
                              {renderMessageSources(msg.id, msg.sources)}
                              {renderMessageContent(msg.content, msg.id)}
                              {renderMessageAttachments(msg.attachments)}
                              {normalizeVisibleMessageContent(msg.content) && (
                                <div className={`mt-4 flex items-center gap-1 ${colors.textMuted}`}>
                                  <button
                                    type="button"
                                    title="复制"
                                    onClick={() => void handleCopy(normalizeVisibleMessageContent(msg.content), `assistant-copy-${msg.id}`)}
                                    className={`rounded-full p-2 ${colors.hover}`}
                                  >
                                    {copiedStates[`assistant-copy-${msg.id}`] ? <CheckCircle2 className="h-4 w-4 text-green-500" /> : <Copy className="h-4 w-4" />}
                                  </button>
                                  <button
                                    type="button"
                                    title="分享"
                                    onClick={() => void handleShareSharedConversationMessage(msg)}
                                    className={`rounded-full p-2 ${colors.hover}`}
                                  >
                                    {copiedStates[`assistant-share-${msg.id}`] ? <CheckCircle2 className="h-4 w-4 text-green-500" /> : <Share2 className="h-4 w-4" />}
                                  </button>
                                </div>
                              )}
                            </div>
                          </div>
                        ) : (
                          <div className={`max-w-[78%] rounded-3xl px-5 py-2.5 text-base leading-relaxed break-words ${colors.userBubble}`}>
                            {renderMessageContent(msg.content, msg.id)}
                            {renderMessageAttachments(msg.attachments)}
                          </div>
                        )}
                      </div>
                    ))}
                    {isSendingSharedMessage && (
                      <div className="flex gap-4 justify-start max-w-full">
                        <div className={`w-8 h-8 rounded-full flex items-center justify-center shrink-0 border ${colors.modalBg} ${colors.border}`}>
                          <img src={BRAND_LOGO_SRC} alt="Infinite-AI" className="h-6 w-6 rounded-xl object-cover" />
                        </div>
                        <div className="flex items-center gap-1.5 pt-3">
                          <div className={`w-2 h-2 rounded-full animate-bounce ${colors.textMuted} bg-current`} />
                          <div className={`w-2 h-2 rounded-full animate-bounce ${colors.textMuted} bg-current`} />
                          <div className={`w-2 h-2 rounded-full animate-bounce ${colors.textMuted} bg-current`} />
                        </div>
                      </div>
                    )}
                    <div ref={messagesEndRef} />
                  </div>
                  <div className={`mt-8 rounded-[28px] border p-4 ${isDark ? 'border-white/8 bg-white/[0.03]' : 'border-black/8 bg-black/[0.02]'}`}>
                    {sharedChatFeedback && (
                      <div className={`mb-3 text-sm ${sharedChatFeedback.includes('失败') || sharedChatFeedback.includes('协作码') || sharedChatFeedback.includes('登录') ? 'text-red-500' : colors.textMuted}`}>
                        {sharedChatFeedback}
                      </div>
                    )}
                    {sharedConversation.collaborationEnabled ? (
                      session?.user ? (
                        canSendSharedConversationMessage ? (
                          <>
                            <textarea
                              value={sharedInputMessage}
                              onChange={(event) => setSharedInputMessage(event.target.value)}
                              onKeyDown={(event) => {
                                if (event.key === 'Enter' && !event.shiftKey) {
                                  event.preventDefault()
                                  void handleSendSharedMessage()
                                }
                              }}
                              placeholder="继续这段共享对话..."
                              className="w-full bg-transparent px-2 py-2 min-h-[56px] max-h-48 resize-none outline-none overflow-y-auto text-base placeholder:text-[#8c8c8c]"
                              rows={1}
                            />
                            <div className="mt-3 flex items-center justify-between gap-3">
                              <div className={`text-xs ${colors.textMuted}`}>协作消息会直接追加到这段共享会话里。</div>
                              <button
                                onClick={() => void handleSendSharedMessage()}
                                disabled={!sharedInputMessage.trim() || isSendingSharedMessage}
                                className={`flex h-10 min-w-24 items-center justify-center rounded-full px-4 text-sm font-medium ${
                                  sharedInputMessage.trim() && !isSendingSharedMessage
                                    ? isDark ? 'bg-white text-black hover:bg-[#ececec]' : 'bg-black text-white hover:bg-[#333]'
                                    : 'bg-gray-500/20 text-gray-500 cursor-not-allowed'
                                }`}
                              >
                                发送
                              </button>
                            </div>
                          </>
                        ) : (
                          <div className="flex flex-col gap-3">
                            {!sharedCollaborationRequested ? (
                              <>
                                <div className={`text-sm ${colors.textMuted}`}>此分享已开启协作，申请后输入协作码即可继续对话。</div>
                                <button
                                  onClick={() => {
                                    setSharedCollaborationRequested(true)
                                    setSharedChatFeedback('')
                                  }}
                                  className={`w-full rounded-full px-4 py-3 text-sm font-medium ${isDark ? 'bg-white text-black hover:bg-[#ececec]' : 'bg-black text-white hover:bg-[#222]'}`}
                                >
                                  申请协作
                                </button>
                              </>
                            ) : (
                              <>
                                <input
                                  value={sharedCollaborationCodeInput}
                                  onChange={(event) => setSharedCollaborationCodeInput(event.target.value)}
                                  placeholder="请输入协作码"
                                  className={`w-full rounded-2xl border px-4 py-3 text-sm outline-none transition ${isDark ? 'border-white/8 bg-white/[0.04] text-white placeholder:text-[#8a8a8a] focus:border-white/20' : 'border-black/10 bg-black/[0.02] text-black placeholder:text-[#999] focus:border-black/20'}`}
                                />
                                <button
                                  onClick={() => void handleJoinSharedCollaboration()}
                                  disabled={isJoiningSharedCollaboration}
                                  className={`w-full rounded-full px-4 py-3 text-sm font-medium ${isDark ? 'bg-white text-black hover:bg-[#ececec]' : 'bg-black text-white hover:bg-[#222]'} ${isJoiningSharedCollaboration ? 'opacity-60 cursor-not-allowed' : ''}`}
                                >
                                  {isJoiningSharedCollaboration ? '验证中...' : '确认协作'}
                                </button>
                              </>
                            )}
                          </div>
                        )
                      ) : (
                        <div className="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
                          <div className={`text-sm ${colors.textMuted}`}>登录后即可参与此对话协作。</div>
                          <button
                            onClick={() => {
                              setIsLoginMode(true)
                              setShowLoginModal(true)
                            }}
                            className={`rounded-full px-4 py-2 text-sm font-medium ${isDark ? 'bg-white text-black hover:bg-[#ececec]' : 'bg-black text-white hover:bg-[#222]'}`}
                          >
                            登录参与协作
                          </button>
                        </div>
                      )
                    ) : (
                      <div className={`text-sm ${colors.textMuted}`}>当前分享仅支持查看，不支持继续协作。</div>
                    )}
                  </div>
                </>
              ) : (
                <div className="flex min-h-[calc(100dvh-140px)] items-center justify-center">
                  <div className={`w-full max-w-[640px] rounded-[32px] border px-6 py-7 shadow-[0_24px_80px_rgba(0,0,0,0.2)] ${isDark ? 'border-white/8 bg-[#2a2a2a]' : 'border-black/8 bg-white'}`}>
                    <div className={`inline-flex items-center gap-2 rounded-full px-3 py-1 text-[11px] font-medium ${isDark ? 'bg-white/6 text-[#cfcfcf]' : 'bg-black/[0.04] text-[#666]'}`}>
                      <ShieldCheck className="h-3.5 w-3.5" />
                      分享不可用
                    </div>
                    <div className="mt-4 text-[28px] font-semibold tracking-[-0.03em]">无法查看这段分享</div>
                    <div className={`mt-2 text-sm leading-6 ${colors.textMuted}`}>这条分享可能已失效、被关闭，或暂时无法访问。</div>
                    {sharedChatFeedback && <div className="mt-4 text-sm text-red-500">{sharedChatFeedback}</div>}
                  </div>
                </div>
              )}
            </div>
          </div>
        </div>
      )}

      {view === 'chat' && (
        <div
          className={`flex h-[100dvh] overflow-hidden relative ${colors.appBg} ${colors.textMain}`}
          onDragEnter={handleComposerDragEnter}
          onDragOver={handleComposerDragOver}
          onDragLeave={handleComposerDragLeave}
          onDrop={handleComposerDrop}
        >
          {isMobileSidebarOpen && <div className="fixed inset-0 bg-black/50 z-40 md:hidden" onClick={() => setIsMobileSidebarOpen(false)} />}
          <div className={`fixed md:relative z-50 h-[100dvh] flex flex-col transition-all duration-300 shrink-0 ${colors.sidebarBg} ${isSidebarOpen ? 'w-[260px]' : 'w-0 overflow-hidden'} ${isMobileSidebarOpen ? 'translate-x-0 w-[260px]' : '-translate-x-full md:translate-x-0'}`}>
            <div className="p-3 flex items-center justify-between">
              <button onClick={() => setIsSidebarOpen(false)} className={`group hidden md:flex p-1.5 rounded-md ${colors.textMuted} ${colors.sidebarHover}`}>
                <div className="relative h-7 w-7">
                  <img src={BRAND_LOGO_SRC} alt="收起侧栏" className="absolute inset-0 h-7 w-7 object-cover transition-opacity group-hover:opacity-0" />
                  <PanelLeftClose className="absolute inset-0 m-auto h-5 w-5 opacity-0 transition-opacity group-hover:opacity-100" />
                </div>
              </button>
              <a href="/?new=1" onClick={startNewChat} className={`flex-1 flex items-center justify-between p-2 rounded-md md:ml-1 ${colors.sidebarHover}`}>
                <div className="flex items-center gap-2">
                  <span className="text-sm font-medium">新聊天</span>
                </div>
                <SquarePen className={`w-4 h-4 ${colors.textMuted}`} />
              </a>
              <button onClick={() => setIsMobileSidebarOpen(false)} className={`md:hidden p-2 rounded-md ${colors.textMuted} ${colors.sidebarHover}`}>
                <X className="w-5 h-5" />
              </button>
            </div>
            <div className="relative z-10 flex-1 overflow-y-auto px-3 py-2 custom-scrollbar">
              <h3 className={`text-xs font-medium px-2 mb-2 mt-2 ${colors.textMuted}`}>{chatHistoryEnabled ? '最近聊天' : '临时聊天'}</h3>
              {!chatHistoryEnabled && (
                <div className={`mx-2 rounded-2xl border px-3 py-3 text-xs leading-5 ${colors.border} ${colors.textMuted}`}>
                  聊天记录已关闭。现在开始的新对话只保留在当前页面，不会进入最近聊天。
                </div>
              )}
              {chatHistoryEnabled && conversations.map((chat) => (
                <div
                  key={chat.id}
                  className={`group flex items-center gap-1 rounded-md px-1 ${
                    currentConversationId === chat.id ? (isDark ? 'bg-[#202123]' : 'bg-[#ececec]') : ''
                  }`}
                >
                  <button
                    type="button"
                    onClick={() => openConversation(chat.id)}
                    className={`relative z-[1] min-w-0 flex-1 text-left px-2 py-2 text-sm rounded-md ${colors.sidebarHover}`}
                    title={chat.title}
                  >
                    <span
                      className="block overflow-hidden text-ellipsis leading-5"
                      style={{
                        display: '-webkit-box',
                        WebkitLineClamp: 2,
                        WebkitBoxOrient: 'vertical',
                      }}
                    >
                      {chat.title}
                    </span>
                  </button>
                  <button
                    type="button"
                    title="删除聊天"
                    aria-label={`删除聊天 ${chat.title}`}
                    disabled={deletingConversationId === chat.id}
                    onClick={(event) => {
                      event.stopPropagation()
                      requestDeleteConversation(chat.id)
                    }}
                    className={`shrink-0 rounded-md p-2 text-gray-500 transition-opacity ${
                      deletingConversationId === chat.id ? 'cursor-not-allowed opacity-100' : 'opacity-100 md:opacity-0 md:group-hover:opacity-100'
                    } ${colors.hover}`}
                  >
                    <Trash2 className="w-4 h-4" />
                  </button>
                </div>
              ))}
            </div>
            <div className="p-3">
              <div className="space-y-1 mb-2">
                <button onClick={() => navigateTo('infinite-code')} className={`w-full flex items-center gap-3 px-2 py-2.5 text-sm rounded-md ${colors.sidebarHover}`}>
                  <Terminal className="w-4 h-4" /> <span>Infinite Code</span>
                </button>
                <button onClick={() => navigateTo('api')} className={`w-full flex items-center gap-3 px-2 py-2.5 text-sm rounded-md ${colors.sidebarHover}`}>
                  <Code2 className="w-4 h-4" /> <span>API 管理</span>
                </button>
                <button onClick={() => navigateTo('download')} className={`w-full flex items-center gap-3 px-2 py-2.5 text-sm rounded-md ${colors.sidebarHover}`}>
                  <Download className="w-4 h-4" /> <span>下载应用</span>
                </button>
              </div>
              {session?.user ? (
                <div className="relative pt-1 border-t border-gray-800/50">
                  <button onClick={() => setIsUserMenuOpen((prev) => !prev)} className={`w-full flex items-center gap-2 p-2 rounded-md ${colors.sidebarHover}`}>
                    <div className="w-8 h-8 rounded-full bg-blue-600 flex items-center justify-center text-white text-xs font-bold">{session.user.displayName?.slice(0, 1) || 'U'}</div>
                    <div className="flex-1 text-left truncate">
                      <div className="text-sm font-medium">{session.user.displayName}</div>
                      <div className={`text-xs mt-0.5 ${colors.textMuted}`}>{activePlanName}</div>
                    </div>
                  </button>
                  {isUserMenuOpen && (
                    <div className={`absolute bottom-[calc(100%+8px)] left-0 w-full border rounded-xl shadow-2xl overflow-hidden z-[200] py-1 ${isDark ? 'bg-[#2f2f2f] border-[#444] text-white' : 'bg-white border-[#e5e5e5] text-black'}`}>
                      <div className={`px-4 py-3 text-sm border-b ${isDark ? 'border-[#444]' : 'border-[#e5e5e5]'}`}>
                        <div className="font-medium">{session.user.displayName}</div>
                        <div className={`text-xs mt-0.5 ${isDark ? 'text-gray-400' : 'text-gray-500'}`}>{session.user.email}</div>
                        <div className={`mt-2 inline-flex rounded-full border px-2.5 py-1 text-[11px] ${colors.border}`}>{activePlanName}</div>
                      </div>
                      <div className="py-1">
                        <button onClick={() => navigateTo('plans')} className={`w-full flex items-center gap-3 px-4 py-2.5 text-sm ${colors.hover}`}>
                          <Zap className="w-4 h-4 text-yellow-500" /> <span>升级套餐</span>
                        </button>
                        <button onClick={() => { setShowSettingsModal(true); setIsUserMenuOpen(false) }} className={`w-full flex items-center gap-3 px-4 py-2.5 text-sm ${colors.hover}`}>
                          <Settings className="w-4 h-4" /> <span>设置</span>
                        </button>
                      </div>
                      <div className={`h-px ${isDark ? 'bg-[#444]' : 'bg-[#e5e5e5]'}`} />
                      <button onClick={handleLogout} className={`w-full flex items-center gap-3 px-4 py-2.5 text-sm text-red-500 ${colors.hover}`}>
                        <LogOut className="w-4 h-4" /> <span>退出登录</span>
                      </button>
                    </div>
                  )}
                </div>
              ) : (
                <div className="flex flex-col gap-2 mt-2 px-1 border-t border-gray-800/50 pt-3">
                  <button onClick={() => { setIsLoginMode(false); setShowLoginModal(true) }} className={`w-full py-2.5 text-sm font-semibold rounded-md ${isDark ? 'bg-white text-black' : 'bg-black text-white'}`}>注册</button>
                  <button onClick={() => { setIsLoginMode(true); setShowLoginModal(true) }} className={`w-full py-2.5 text-sm font-semibold rounded-md border ${colors.border}`}>登录</button>
                </div>
              )}
            </div>
          </div>

          <div className="flex-1 flex flex-col h-[100dvh] relative min-w-0">
            <div className={`h-14 flex items-center px-2 md:px-4 shrink-0 gap-2 sticky top-0 z-20 ${colors.appBg}`}>
              {!isSidebarOpen && (
                <button onClick={() => setIsSidebarOpen(true)} className={`group hidden md:flex p-1.5 rounded-md ${colors.textMuted} ${colors.hover}`}>
                  <div className="relative h-7 w-7">
                    <img src={BRAND_LOGO_SRC} alt="展开侧栏" className="absolute inset-0 h-7 w-7 object-cover transition-opacity group-hover:opacity-0" />
                    <PanelLeft className="absolute inset-0 m-auto h-5 w-5 opacity-0 transition-opacity group-hover:opacity-100" />
                  </div>
                </button>
              )}
              <button className={`md:hidden p-2 rounded-md ${colors.textMuted} ${colors.hover}`} onClick={() => setIsMobileSidebarOpen(true)}>
                <Menu className="w-6 h-6" />
              </button>
              <div className="relative">
                <button onClick={() => setIsModelSelectorOpen((prev) => !prev)} className={`flex items-center gap-1.5 text-lg font-medium px-2 py-1.5 rounded-md ${colors.hover}`}>
                  <span className="opacity-90">{getModelLabel(selectedModel)}</span>
                  <ChevronDown className="w-4 h-4 text-gray-500" />
                </button>
                {isModelSelectorOpen && (
                  <div className={`absolute top-full left-0 mt-1 w-72 border rounded-xl shadow-2xl z-50 py-2 ${colors.modalInner} ${colors.border}`}>
                    {modelOptions.map((option) => (
                      <button key={option.slug} onClick={() => handleSelectModel(option)} className={`w-full flex items-center justify-between px-4 py-3 ${colors.hover}`}>
                        <div className="flex flex-col text-left">
                          <span className="text-sm font-medium">{getModelLabel(option)}</span>
                          <span className={`text-xs ${colors.textMuted}`}>{option.desc ?? option.description}</span>
                        </div>
                        {selectedModel?.slug === option.slug && <Check className="w-4 h-4" />}
                      </button>
                    ))}
                  </div>
                )}
              </div>
              <div className="flex-1" />
              {currentConversationId && messages.length > 0 && (
                <button onClick={() => void openShareModal()} className={`flex items-center gap-2 rounded-full border px-3 py-1.5 text-sm ${colors.border} ${colors.hover}`}>
                  <Share2 className="h-4 w-4" />
                  分享
                </button>
              )}
            </div>
            <div className="flex-1 overflow-y-auto px-4 pb-4">
              {messages.length === 0 ? (
                <div className="h-full flex flex-col items-center justify-center max-w-3xl mx-auto text-center">
                  <img src={BRAND_LOGO_SRC} alt="Infinite-AI" className="h-24 w-24 rounded-[2rem] object-cover mb-6" />
                  <h1 className="text-2xl font-medium mb-8">有什么我可以帮忙的？</h1>
                  {!chatHistoryEnabled && <p className={`max-w-xl text-sm leading-6 ${colors.textMuted}`}>当前为临时聊天模式。新的消息会保留在这个页面里，但不会写入你的账号历史。</p>}
                </div>
              ) : (
                <div className="max-w-3xl mx-auto space-y-6 pt-4">
                  {messages.map((msg) => (
                    <div id={`message-${msg.id}`} key={msg.id} className={`flex w-full ${msg.role === 'user' ? 'justify-end' : 'justify-start'}`}>
                      {isAssistantRole(msg.role) ? (
                        <div className="flex gap-4 max-w-full">
                          <div className={`w-8 h-8 rounded-full flex items-center justify-center shrink-0 border ${colors.modalBg} ${colors.border}`}>
                            <img src={BRAND_LOGO_SRC} alt="Infinite-AI" className="h-6 w-6 rounded-xl object-cover" />
                          </div>
                          <div className="pt-1.5 min-w-0">
                            {renderReasoningPanel(msg)}
                            {renderMessageSources(msg.id, msg.sources)}
                            {renderMessageContent(msg.content, msg.id)}
                            {renderMessageAttachments(msg.attachments)}
                            {renderMessageArtifacts(msg.artifacts)}
                            {normalizeVisibleMessageContent(msg.content) && (
                              <div className={`mt-4 flex items-center gap-1 ${colors.textMuted}`}>
                                <button
                                  type="button"
                                  title="复制"
                                  onClick={() => void handleCopy(normalizeVisibleMessageContent(msg.content), `assistant-copy-${msg.id}`)}
                                  className={`rounded-full p-2 ${colors.hover}`}
                                >
                                  {copiedStates[`assistant-copy-${msg.id}`] ? <CheckCircle2 className="h-4 w-4 text-green-500" /> : <Copy className="h-4 w-4" />}
                                </button>
                                <button
                                  type="button"
                                  title="分享"
                                  onClick={() => void handleShareConversationMessage(msg)}
                                  className={`rounded-full p-2 ${colors.hover}`}
                                >
                                  {copiedStates[`assistant-share-${msg.id}`] ? <CheckCircle2 className="h-4 w-4 text-green-500" /> : <Share2 className="h-4 w-4" />}
                                </button>
                              </div>
                            )}
                          </div>
                        </div>
                      ) : (
                        <div className="group flex max-w-[75%] flex-col items-end gap-2">
                          <div
                            className={`rounded-3xl px-5 py-2.5 text-base leading-relaxed break-words ${colors.userBubble} ${
                              editingMessageId === msg.id ? 'ring-1 ring-blue-500/50' : ''
                            }`}
                          >
                            {renderMessageContent(msg.content, msg.id)}
                            {renderMessageAttachments(msg.attachments)}
                            {renderMessageArtifacts(msg.artifacts)}
                          </div>
                          <div
                            className={`flex items-center gap-1 rounded-full border px-2 py-1 text-xs transition-opacity ${
                              isDark ? 'bg-[#262626]' : 'bg-white'
                            } ${colors.border} opacity-100 md:opacity-0 md:group-hover:opacity-100`}
                          >
                            <button
                              type="button"
                              title="复制"
                              onClick={() => handleCopy(msg.content || '', `message-copy-${msg.id}`)}
                              className={`flex items-center gap-1 rounded-full px-2 py-1 ${colors.hover}`}
                            >
                              {copiedStates[`message-copy-${msg.id}`] ? <CheckCircle2 className="w-3.5 h-3.5 text-green-500" /> : <Copy className="w-3.5 h-3.5" />}
                              <span>复制</span>
                            </button>
                            <button
                              type="button"
                              title="重新编辑"
                              onClick={() => handleEditMessage(msg)}
                              className={`flex items-center gap-1 rounded-full px-2 py-1 ${colors.hover}`}
                            >
                              <SquarePen className="w-3.5 h-3.5" />
                              <span>重新编辑</span>
                            </button>
                          </div>
                        </div>
                      )}
                    </div>
                  ))}
                  {showGlobalTypingIndicator && (
                    <div className="flex gap-4 justify-start max-w-full">
                      <div className={`w-8 h-8 rounded-full flex items-center justify-center shrink-0 border ${colors.modalBg} ${colors.border}`}>
                        <img src={BRAND_LOGO_SRC} alt="Infinite-AI" className="h-6 w-6 rounded-xl object-cover" />
                      </div>
                      <div className="flex flex-col gap-2 pt-3 min-w-0">
                        {isDeepSearchThinking && (
                          <div className={`rounded-2xl border px-4 py-3 text-sm ${colors.border} ${colors.textMuted}`}>
                            正在整理深度搜索思考内容...
                          </div>
                        )}
                        <div className="flex items-center gap-1.5">
                          <div className={`w-2 h-2 rounded-full animate-bounce ${colors.textMuted} bg-current`} />
                          <div className={`w-2 h-2 rounded-full animate-bounce ${colors.textMuted} bg-current`} />
                          <div className={`w-2 h-2 rounded-full animate-bounce ${colors.textMuted} bg-current`} />
                        </div>
                      </div>
                    </div>
                  )}
                  <div ref={messagesEndRef} />
                </div>
              )}
            </div>
            <div className={`shrink-0 px-4 pb-6 ${colors.appBg}`}>
              <div className="max-w-3xl mx-auto">
                <div
                  className={`relative rounded-[24px] border transition-all ${colors.inputBg} ${
                    isComposerDragActive
                      ? isDark
                        ? 'border-blue-400/70 ring-4 ring-blue-500/15'
                        : 'border-blue-500/70 ring-4 ring-blue-500/12'
                      : 'border-transparent'
                  }`}
                >
                  {isComposerDragActive && (
                    <div className={`pointer-events-none absolute inset-0 z-10 flex items-center justify-center rounded-[24px] border-2 border-dashed ${isDark ? 'border-blue-300/70 bg-[#1d3557]/35 text-blue-100' : 'border-blue-500/70 bg-blue-50/85 text-blue-700'}`}>
                      <div className="rounded-full px-4 py-2 text-sm font-medium backdrop-blur">
                        松开即可添加文件或图片
                      </div>
                    </div>
                  )}
                  {(editingMessageId || chatFeedback) && (
                    <div className="px-5 pt-4">
                      {editingMessageId && (
                        <div className={`mb-3 flex items-center justify-between gap-3 rounded-2xl border px-4 py-3 text-sm ${colors.border} ${isDark ? 'bg-[#262626]' : 'bg-[#fafafa]'}`}>
                          <span>正在重新编辑这条问题，发送后会从这里重新生成后续回答。</span>
                          <button type="button" onClick={cancelEditingMessage} className={`shrink-0 rounded-full px-3 py-1 text-xs ${colors.hover}`}>
                            取消
                          </button>
                        </div>
                      )}
                      {chatFeedback && (
                        <div className={`mb-3 flex items-start justify-between gap-3 rounded-2xl border px-4 py-3 text-sm ${colors.border} ${isDark ? 'bg-[#262626]' : 'bg-[#fafafa]'} ${chatFeedback.includes('失败') || chatFeedback.includes('断联') ? 'text-red-500' : colors.textMuted}`}>
                          <span className="min-w-0 flex-1 leading-6">{chatFeedback}</span>
                          <button
                            type="button"
                            onClick={() => setChatFeedback('')}
                            className={`shrink-0 rounded-full p-1 ${colors.hover}`}
                            title="关闭提示"
                          >
                            <X className="h-4 w-4" />
                          </button>
                        </div>
                      )}
                    </div>
                  )}
                  {composerAttachments.length > 0 && (
                    <div className="flex flex-wrap gap-3 px-5 pt-4">
                      {composerAttachments.map((item) => (
                        isImageMimeType(item.mimeType) && item.previewUrl ? (
                          <div key={item.clientId} className={`group relative h-24 w-24 overflow-hidden rounded-[20px] border shadow-sm ${colors.border} ${isDark ? 'bg-[#171717]' : 'bg-white'}`}>
                            <img src={item.previewUrl} alt={item.fileName} className="h-full w-full object-cover" />
                            <button
                              type="button"
                              onClick={() => removeComposerAttachment(item.clientId)}
                              className="absolute right-1.5 top-1.5 flex h-7 w-7 items-center justify-center rounded-full bg-black/80 text-white shadow-lg transition-transform hover:scale-105"
                              title="移除附件"
                            >
                              <X className="h-4 w-4" />
                            </button>
                            {item.status !== 'ready' && (
                              <div className="absolute inset-x-1.5 bottom-1.5 rounded-full bg-black/70 px-2 py-1 text-center text-[11px] text-white backdrop-blur">
                                {item.status === 'uploading' ? '上传中...' : item.error || '上传失败'}
                              </div>
                            )}
                          </div>
                        ) : (
                          <div key={item.clientId} className={`flex max-w-[260px] items-center gap-3 rounded-2xl border px-3 py-2 text-xs ${colors.border} ${isDark ? 'bg-[#242424]' : 'bg-white'}`}>
                            <div className={`flex h-9 w-9 shrink-0 items-center justify-center rounded-xl ${isDark ? 'bg-white/8' : 'bg-black/[0.04]'}`}>
                              <Paperclip className="h-4 w-4" />
                            </div>
                            <div className="min-w-0 flex-1">
                              <div className="truncate text-sm">{item.fileName}</div>
                              <div className={`${item.status === 'error' ? 'text-red-500' : colors.textMuted}`}>
                                {item.status === 'uploading' ? '上传中...' : item.status === 'ready' ? formatFileSize(item.sizeBytes) || '已添加' : item.error || '上传失败'}
                              </div>
                            </div>
                            <button type="button" onClick={() => removeComposerAttachment(item.clientId)} className={`shrink-0 rounded-full p-1 ${colors.hover}`}>
                              <X className="h-4 w-4" />
                            </button>
                          </div>
                        )
                      ))}
                    </div>
                  )}
                  <textarea
                    ref={composerTextareaRef}
                    value={inputMessage}
                    onChange={(event) => {
                      setInputMessage(event.target.value)
                      if (chatFeedback) {
                        setChatFeedback('')
                      }
                    }}
                    onKeyDown={(event) => {
                      if (event.key === 'Enter' && !event.shiftKey) {
                        event.preventDefault()
                        void handleSendMessage()
                      }
                    }}
                    onPaste={handleComposerPaste}
                    placeholder="发送消息..."
                    className="w-full bg-transparent px-5 py-4 min-h-[56px] max-h-48 resize-none outline-none overflow-y-auto text-base placeholder-gray-500"
                    rows={1}
                  />
                  <div className="flex items-center justify-between px-3 pb-3">
                    <div className="flex items-center gap-1">
                      <input type="file" multiple ref={fileInputRef} className="hidden" onChange={(event) => void handleAttachmentFiles(event.target.files)} />
                      <input type="file" multiple accept="image/*" ref={imageInputRef} className="hidden" onChange={(event) => void handleAttachmentFiles(event.target.files)} />
                      <button type="button" onClick={() => fileInputRef.current?.click()} className={`p-2 rounded-full text-gray-500 ${colors.hover}`}><Paperclip className="w-5 h-5" /></button>
                      <button type="button" onClick={() => imageInputRef.current?.click()} className={`p-2 rounded-full text-gray-500 ${colors.hover}`}><ImageIcon className="w-5 h-5" /></button>
                      <button
                        type="button"
                        onClick={() => {
                          const nextValue = !isDeepSearch
                          storeDeepSearchPreference(nextValue)
                          setIsDeepSearch(nextValue)
                          setModelLimitState(null)
                        }}
                        className={`flex items-center gap-1.5 ml-1 px-3 py-1.5 rounded-full text-sm font-medium ${isDeepSearch ? 'text-blue-500 bg-blue-500/10' : `text-gray-500 ${colors.hover}`}`}
                      >
                        <Globe className="w-4 h-4" />
                        <span className="hidden sm:inline">深度搜索</span>
                      </button>
                    </div>
                    <button
                      type="button"
                      onClick={() => {
                        if (showStopButton) {
                          cancelVisibleRequest({ waitForServer: true })
                          return
                        }
                        void handleSendMessage()
                      }}
                      disabled={!showStopButton && !canSendMessage}
                      title={showStopButton ? '停止输出' : '发送'}
                      className={`flex h-9 w-9 shrink-0 items-center justify-center rounded-xl ${
                        showStopButton || canSendMessage
                          ? isDark ? 'bg-white text-black hover:bg-[#ececec]' : 'bg-black text-white hover:bg-[#333]'
                          : 'bg-gray-500/20 text-gray-500 cursor-not-allowed'
                      }`}
                    >
                      {showStopButton ? <span className="h-3.5 w-3.5 rounded-[3px] bg-current" /> : <ArrowUp className="w-5 h-5" />}
                    </button>
                  </div>
                </div>
                {modelLimitState && (
                  <div className={`mt-3 flex flex-col gap-3 rounded-[22px] border px-4 py-4 sm:flex-row sm:items-center sm:justify-between ${colors.border} ${isDark ? 'bg-[#171717]' : 'bg-[#fafafa]'}`}>
                    <div className="min-w-0">
                      <div className="text-sm font-medium">
                        {modelLimitState.reason === 'unavailable'
                          ? `${getPlanLabel(modelLimitState.planCode)} 当前暂不支持使用 ${modelLimitState.modelName || getModelLabel(selectedModel)}`
                          : `${modelLimitState.modelName || getModelLabel(selectedModel)} 的 ${modelLimitState.windowHours || 24} 小时回复额度已用完`}
                      </div>
                      <div className={`mt-1 text-xs leading-5 ${colors.textMuted}`}>
                        {modelLimitState.reason === 'unavailable'
                          ? '可以升级套餐后继续使用这个模型。'
                          : `仅成功回复才会计入次数。你当前已使用 ${modelLimitState.used}/${modelLimitState.limit} 次，请升级或等待 24 小时后再试。`}
                      </div>
                    </div>
                    <div className="flex items-center gap-2 shrink-0">
                      <button onClick={() => navigateTo('plans')} className={`rounded-full px-4 py-2 text-sm font-medium ${colors.btnPrimary}`}>
                        升级套餐
                      </button>
                      <button onClick={() => setModelLimitState(null)} className={`rounded-full border px-4 py-2 text-sm font-medium ${colors.border} ${colors.hover}`}>
                        知道了
                      </button>
                    </div>
                  </div>
                )}
                <div className={`text-center text-xs mt-2 ${colors.textMuted}`}>Infinite-AI 会产生错误。请核查重要信息。</div>
              </div>
            </div>
          </div>
        </div>
      )}
    </>
  )
}
