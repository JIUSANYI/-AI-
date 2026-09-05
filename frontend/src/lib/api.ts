import type { ApiEnvelope, ApiError, AuthPayload, Question, Pagination, TokenPayload, User } from "./types";

const API_BASE = (process.env.NEXT_PUBLIC_API_BASE_URL || "/api/v1").replace(/\/$/, "");
let accessToken: string | null = null;
let refreshInFlight: Promise<string | null> | null = null;

export class ApiRequestError extends Error { constructor(public status: number, public code: string, message: string) { super(message); } }
export function setAccessToken(token: string | null) { accessToken = token; }
export function getAccessToken() { return accessToken; }

async function request<T>(path: string, init: RequestInit = {}, retry = true, attempt = 0, timeoutMs = 15_000): Promise<T> {
  const headers = new Headers(init.headers);
  if (init.body && !headers.has("Content-Type")) headers.set("Content-Type", "application/json");
  if (accessToken) headers.set("Authorization", `Bearer ${accessToken}`);
  const isRefresh = path === "/auth/refresh";
  if (isRefresh || path === "/auth/logout") headers.set("X-CSRF-Protection", "1");
  let response: Response;
  const controller = new AbortController();
  const timeout = globalThis.setTimeout(() => controller.abort(), timeoutMs);
  try { response = await fetch(`${API_BASE}${path}`, { ...init, headers, credentials: "include", signal: controller.signal }); }
  catch (error) {
    if (error instanceof DOMException && error.name === "AbortError") throw new ApiRequestError(408, "REQUEST_TIMEOUT", "请求处理时间较长，请稍后查看历史记录");
    if (attempt < 2 && (init.method || "GET").toUpperCase() === "GET") return request<T>(path, init, retry, attempt + 1, timeoutMs);
    throw error;
  } finally { globalThis.clearTimeout(timeout); }
  const payload = await response.json().catch(() => ({} as ApiError));
  if (response.status === 401 && retry && !isRefresh && path !== "/auth/logout") {
    const refreshed = await refreshAccessToken();
    if (refreshed) return request<T>(path, init, false, 0, timeoutMs);
  }
  if (response.status >= 500 && attempt < 2 && (init.method || "GET").toUpperCase() === "GET") return request<T>(path, init, retry, attempt + 1, timeoutMs);
  if (!response.ok) {
    const error = payload as ApiError;
    throw new ApiRequestError(response.status, error.error?.code || "REQUEST_FAILED", error.error?.message || "请求失败，请稍后重试");
  }
  return (payload as ApiEnvelope<T>).data;
}

export async function refreshAccessToken(): Promise<string | null> {
  if (!refreshInFlight) refreshInFlight = request<TokenPayload>("/auth/refresh", { method: "POST" }, false).then((data) => { setAccessToken(data.access_token); return data.access_token; }).catch(() => { setAccessToken(null); return null; }).finally(() => { refreshInFlight = null; });
  return refreshInFlight;
}
function questionPathSegment(id: number | string) { return encodeURIComponent(String(id)); }
export const api = {
  sendSmsCode: (phone: string) => request<{ expires_in: number; resend_after: number }>("/auth/sms-code", { method: "POST", body: JSON.stringify({ phone, purpose: "login" }) }),
  login: (phone: string, code: string) => request<AuthPayload>("/auth/login", { method: "POST", body: JSON.stringify({ phone, code }) }),
  logout: () => request<{ logged_out: boolean }>("/auth/logout", { method: "POST" }),
  me: () => request<User>("/auth/me"),
  createQuestion: (content: string) => request<Question>("/questions", { method: "POST", body: JSON.stringify({ content }) }, true, 0, 135_000),
  retryQuestion: (id: number | string) => request<Question>(`/questions/${questionPathSegment(id)}/retry`, { method: "POST" }),
  listQuestions: (page = 1, size = 20) => request<{ items: Question[]; pagination: Pagination }>(`/questions?page=${page}&size=${size}`),
  getQuestion: (id: number | string) => request<Question>(`/questions/${questionPathSegment(id)}`),
  health: () => request<{ status: string }>("/health"),
};
