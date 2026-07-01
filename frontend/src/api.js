const BASE = import.meta.env.VITE_API_URL || "http://127.0.0.1:8081";

export async function apiFetch(path, options = {}) {
  const token = localStorage.getItem("token");

  const headers = { ...(options.headers || {}) };

  if (token) {
    headers["Authorization"] = `Bearer ${token}`;
  }

  // Only set Content-Type to JSON if we're NOT sending FormData
  if (!(options.body instanceof FormData)) {
    headers["Content-Type"] = headers["Content-Type"] || "application/json";
  }

  let response;
  try {
    response = await fetch(`${BASE}${path}`, { ...options, headers });
  } catch (err) {
    // Network error (backend is down, CORS blocked before response, etc.)
    throw new Error("Cannot connect to server. Check if Go backend is running.");
  }

  // Auto-logout on 401
  if (response.status === 401) {
    localStorage.removeItem("token");
    localStorage.removeItem("user");
    localStorage.removeItem("adminVerified");
    window.dispatchEvent(new Event("authChange"));
    window.location.href = "/login";
    return response;
  }

  return response;
}

// ── Helpers ───────────────────────────────────────────────────────────────────

/** POST JSON */
export async function apiPost(path, body) {
  return apiFetch(path, {
    method: "POST",
    body: JSON.stringify(body),
  });
}

/** POST FormData (file uploads, multipart forms) */
export async function apiPostForm(path, formData) {
  return apiFetch(path, {
    method: "POST",
    body: formData,
  });
}

/** GET */
export async function apiGet(path) {
  return apiFetch(path, { cache: "no-store" });
}

/** DELETE */
export async function apiDelete(path) {
  return apiFetch(path, { method: "DELETE" });
}

// ── Auth helpers ──────────────────────────────────────────────────────────────

/** Returns the stored user object or null */
export function getUser() {
  try {
    return JSON.parse(localStorage.getItem("user"));
  } catch {
    return null;
  }
}

/** Returns true if a token exists in localStorage */
export function isLoggedIn() {
  return !!localStorage.getItem("token");
}

/** Clears auth state and redirects to login */
export function logout() {
  localStorage.removeItem("token");
  localStorage.removeItem("user");
  localStorage.removeItem("adminVerified");
  window.dispatchEvent(new Event("authChange"));
  window.location.href = "/login";
}