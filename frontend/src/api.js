const BASE_URL = (import.meta.env.VITE_API_URL || "/api").replace(/\/$/, "");
const REQUEST_TIMEOUT_MS = 15_000;

function buildUrl(path) {
  return `${BASE_URL}/${String(path).replace(/^\//, "")}`;
}

export async function apiFetch(path, options = {}) {
  const token = localStorage.getItem("token");
  const headers = new Headers(options.headers || {});

  if (token) {
    headers.set("Authorization", `Bearer ${token}`);
  }

  if (options.body != null && !(options.body instanceof FormData) && !headers.has("Content-Type")) {
    headers.set("Content-Type", "application/json");
  }

  const controller = new AbortController();
  const timeoutId = window.setTimeout(() => controller.abort(), REQUEST_TIMEOUT_MS);
  const externalSignal = options.signal;
  const abortExternal = () => controller.abort();
  externalSignal?.addEventListener("abort", abortExternal, { once: true });

  try {
    return await fetch(buildUrl(path), {
      ...options,
      headers,
      signal: controller.signal,
    });
  } catch (error) {
    if (error?.name === "AbortError") {
      throw new Error("Request timed out. Check the server and local network connection.");
    }
    throw new Error("Cannot connect to the server. Check that the contest monitor is running on this network.");
  } finally {
    window.clearTimeout(timeoutId);
    externalSignal?.removeEventListener("abort", abortExternal);
  }
}

async function readJson(response) {
  try {
    return await response.json();
  } catch {
    return null;
  }
}

export async function apiJson(path, options = {}) {
  const response = await apiFetch(path, options);
  if (response.status === 401) {
    logout();
  }
  return { response, data: await readJson(response) };
}

export async function apiPost(path, body) {
  return apiFetch(path, {
    method: "POST",
    body: JSON.stringify(body),
  });
}

export async function apiPostForm(path, formData) {
  return apiFetch(path, {
    method: "POST",
    body: formData,
  });
}

export async function apiGet(path, options = {}) {
  return apiFetch(path, { ...options, method: "GET", cache: "no-store" });
}

export async function apiDelete(path) {
  return apiFetch(path, { method: "DELETE" });
}

export function getUser() {
  try {
    return JSON.parse(localStorage.getItem("user"));
  } catch {
    return null;
  }
}

export function isLoggedIn() {
  return Boolean(localStorage.getItem("token"));
}

export function logout() {
  localStorage.removeItem("token");
  localStorage.removeItem("user");
  window.dispatchEvent(new Event("authChange"));
  window.location.assign("/login");
}
