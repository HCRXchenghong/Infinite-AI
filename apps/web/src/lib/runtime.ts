function replacePort(defaultURL: string, targetPort: string) {
  if (typeof window === 'undefined') {
    return defaultURL
  }
  const nextURL = new URL(window.location.href)
  nextURL.port = targetPort
  nextURL.pathname = ''
  nextURL.search = ''
  nextURL.hash = ''
  return nextURL.origin
}

function normalizedBaseURL(explicit: string | undefined, fallbackPort: string, defaultURL: string) {
  return explicit?.replace(/\/+$/, '') || replacePort(defaultURL, fallbackPort)
}

export function getUserAppBaseURL() {
  return normalizedBaseURL(import.meta.env.VITE_USER_APP_BASE_URL, '1001', 'http://127.0.0.1:1001')
}

export function getPublicAPIBaseURL() {
  if (typeof window !== 'undefined') {
    return import.meta.env.VITE_PUBLIC_API_BASE_URL?.replace(/\/+$/, '') || window.location.origin
  }
  return normalizedBaseURL(import.meta.env.VITE_PUBLIC_API_BASE_URL, '1001', 'http://127.0.0.1:1001')
}
