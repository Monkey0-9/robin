import { create } from 'zustand';

const GATEWAY_URL = process.env.NEXT_PUBLIC_GATEWAY_URL || 'http://localhost:8080';

interface AuthState {
  token: string | null;
  role: string | null;
  username: string | null;
  expiresAt: number | null;
  isLoading: boolean;
  error: string | null;

  login: (username: string, password: string) => Promise<boolean>;
  logout: () => void;
  isAuthenticated: () => boolean;
  getToken: () => string | null;
}

export const useAuthStore = create<AuthState>((set, get) => ({
  // In-memory only — never persisted to localStorage or cookies
  token: null,
  role: null,
  username: null,
  expiresAt: null,
  isLoading: false,
  error: null,

  login: async (username: string, password: string): Promise<boolean> => {
    set({ isLoading: true, error: null });

    try {
      const res = await fetch(`${GATEWAY_URL}/api/auth/login`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ username, password }),
        signal: AbortSignal.timeout(5000),
      });

      if (!res.ok) {
        const body = await res.json().catch(() => ({}));
        set({
          isLoading: false,
          error: body?.error || 'Login failed. Check credentials.',
        });
        return false;
      }

      const data = await res.json();
      set({
        token: data.token,
        role: data.role,
        username: data.sub,
        expiresAt: data.expires_at,
        isLoading: false,
        error: null,
      });
      return true;
    } catch (e: unknown) {
      const msg = e instanceof Error ? e.message : 'Network error — is the gateway running?';
      set({ isLoading: false, error: msg });
      return false;
    }
  },

  logout: () => {
    set({ token: null, role: null, username: null, expiresAt: null, error: null });
  },

  isAuthenticated: () => {
    const { token, expiresAt } = get();
    if (!token) return false;
    if (expiresAt && Date.now() / 1000 > expiresAt) {
      // Token expired — clear silently
      set({ token: null, role: null, username: null, expiresAt: null });
      return false;
    }
    return true;
  },

  getToken: () => {
    const { isAuthenticated, token } = get();
    if (!isAuthenticated()) return null;
    return token;
  },
}));
