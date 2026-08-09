import { create } from 'zustand';

const GATEWAY_URL = process.env.NEXT_PUBLIC_GATEWAY_URL || 'http://localhost:8080';

// ── Demo users (gateway-offline fallback) ────────────────────────────────────
// These allow previewing the full UI without a running backend.
// In production, these never match because the real gateway responds first.
const DEMO_USERS: Record<string, { password: string; role: string }> = {
  admin:  { password: 'admin',  role: 'ADMIN'  },
  trader: { password: 'trader', role: 'TRADER' },
};

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
        signal: AbortSignal.timeout(4000),
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
    } catch {
      // Gateway offline — attempt demo-mode login with local credentials
      const demo = DEMO_USERS[username.toLowerCase()];
      if (demo && demo.password === password) {
        const fakeToken = `demo.${btoa(JSON.stringify({ sub: username, role: demo.role }))}.demo`;
        set({
          token: fakeToken,
          role: demo.role,
          username,
          expiresAt: Date.now() + 8 * 60 * 60 * 1000, // 8h session
          isLoading: false,
          error: null,
        });
        console.info('[Robin] Gateway offline — Demo mode active');
        return true;
      }
      set({
        isLoading: false,
        error: 'Gateway unreachable. Use admin/admin for demo mode.',
      });
      return false;
    }
  },

  logout: () => {
    set({ token: null, role: null, username: null, expiresAt: null, error: null });
  },

  isAuthenticated: () => {
    const { token, expiresAt } = get();
    if (!token) return false;
    // Demo tokens are strings, not JWT exp timestamps — skip expiry check for them
    if (token.startsWith('demo.')) return true;
    if (expiresAt && Date.now() > expiresAt) {
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
