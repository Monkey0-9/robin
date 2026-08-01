"use client";
import { useEffect } from 'react';

export default function FetchInterceptor() {
  useEffect(() => {
    if (typeof window !== 'undefined' && !(window as any)._fetchIntercepted) {
      const originalFetch = window.fetch;
      window.fetch = async function (...args) {
        const [resource, config] = args;
        const newConfig: RequestInit = { ...config, credentials: 'include' };
        
        // Strip out the JWT Bearer header if it exists (forcing httpOnly cookie usage)
        if (newConfig.headers) {
          const headers = new Headers(newConfig.headers);
          if (headers.has('Authorization')) {
            headers.delete('Authorization');
          }
          newConfig.headers = headers;
        }

        return originalFetch(resource, newConfig);
      };
      (window as any)._fetchIntercepted = true;
    }
  }, []);
  return null;
}
